import importlib.util
import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

from scripts.console_browser_test import DevTools, MAX_FRAME_BYTES
from scripts import console_browser_test as browser
from scripts.verification_process import process_exited, run_owned


class ResourceTests(unittest.TestCase):
    def test_go_toolchain_pins_match_security_floor(self):
        root = Path(__file__).resolve().parent.parent
        version = '1.26.10'
        self.assertIn('go ' + version, (root / 'go.mod').read_text().splitlines())
        self.assertIn('export GOTOOLCHAIN := go' + version,
                      (root / 'Makefile').read_text().splitlines())
        for workflow in ('ci.yml', 'release.yml'):
            with self.subTest(workflow=workflow):
                self.assertIn('  GO_VERSION: "' + version + '"',
                              (root / '.github/workflows' / workflow).read_text().splitlines())
        self.assertIn('  GOTOOLCHAIN: "go' + version + '"',
                      (root / '.github/workflows/ci.yml').read_text().splitlines())
        for document in ('README.md', 'CONTRIBUTING.md', 'docs/QUICKSTART.md',
                         'docs/DEMO_NO_KEY.md'):
            with self.subTest(document=document):
                self.assertIn('Go ' + version, (root / document).read_text())

    def test_ci_hardens_go_before_verification(self):
        root = Path(__file__).resolve().parent.parent
        for workflow in ('ci.yml', 'release.yml'):
            text = (root / '.github/workflows' / workflow).read_text()
            # Every setup-go job must remove archive-supplied write access
            # before any product tests apply the executable trust policy.
            jobs = text.split('      - uses: actions/setup-go@v6')[1:]
            self.assertTrue(jobs)
            for job in jobs:
                expected = ('run: python3 scripts/prepare-ci-go-toolchain.py --goroot "$(go env GOROOT)" --version "$GO_VERSION"'
                            if workflow == 'ci.yml' else
                            'run: chmod go-w "$(go env GOROOT)/bin/go"')
                self.assertIn(expected, job.split('      - name: Prepare verification')[0])

    def client(self):
        client = DevTools.__new__(DevTools)
        client.sock = mock.Mock()
        client.next_id = 1
        client.events = []
        client.event_bytes = 0
        client.deadline = time.monotonic() + 1
        return client

    def test_oversized_frame_rejected_before_payload_read(self):
        client = self.client()
        client._recv_exact = mock.Mock(side_effect=[bytes([0x81, 127]), struct.pack('!Q', MAX_FRAME_BYTES+1)])
        with self.assertRaisesRegex(RuntimeError, 'frame exceeds'):
            client.receive()
        self.assertEqual(client._recv_exact.call_count, 2)

    def test_event_flood_is_not_silently_truncated(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'method': 'noise'})
        with mock.patch.object(browser, 'MAX_EVENTS', 2):
            with self.assertRaisesRegex(RuntimeError, 'event budget'):
                client.command('test')
        self.assertEqual(len(client.events), 2)

    def test_event_byte_budget(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'method': 'large event'})
        with mock.patch.object(browser, 'MAX_EVENT_BYTES', 1):
            with self.assertRaisesRegex(RuntimeError, 'event budget'):
                client.command('test')

    def test_absolute_deadline_even_when_messages_keep_arriving(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'method': 'noise'})
        with mock.patch.object(browser, 'CDP_TIMEOUT', 0.01):
            with self.assertRaisesRegex(RuntimeError, 'deadline'):
                client.command('test')

    def test_slow_frame_uses_remaining_deadline(self):
        client = self.client()
        def recv(size):
            time.sleep(.02)
            return b'x'
        client.sock.recv.side_effect = recv
        client.deadline = time.monotonic() + .01
        with self.assertRaisesRegex(RuntimeError, 'deadline'):
            client._recv_exact(2)

    def paced_client(self):
        client = self.client()
        client._frame = lambda payload: payload
        client.receive = mock.Mock(return_value={'id': 1, 'result': {}})
        client.pace_fixture_requests('http://fixture:8450')
        self.assertEqual(json.loads(client.sock.sendall.call_args.args[0]), {
            'id': 1, 'method': 'Fetch.enable', 'params': {'patterns': [
                {'urlPattern': 'http://fixture:8450/*', 'requestStage': 'Request'}]}})
        client.sock.sendall.reset_mock()
        return client

    def paused(self, identifier='intercept', resource='Document', method='GET',
               url='http://fixture:8450/console?private=yes'):
        return {'method': 'Fetch.requestPaused', 'params': {
            'requestId': identifier, 'resourceType': resource,
            'request': {'url': url, 'method': method, 'postData': 'private body',
                        'headers': {'Cookie': 'private cookie'}}}}

    def test_all_resource_pacing_preserves_requests_and_interleaved_replies(self):
        for outer_first in (False, True):
            with self.subTest(outer_first=outer_first):
                now = [0.0]
                releases = []
                def sleep(delay):
                    now[0] += delay
                with (mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]),
                      mock.patch.object(browser.time, 'sleep', side_effect=sleep)):
                    client = self.paced_client()
                    pauses = [self.paused(str(i), kind, verb) for i, (kind, verb) in enumerate([
                        ('Document', 'GET'), ('Stylesheet', 'GET'), ('Script', 'GET'),
                        ('XHR', 'GET'), ('XHR', 'POST')])]
                    original = json.dumps(pauses)
                    # All continuations remain outstanding as the outer reply
                    # arrives; both reply orderings must retain its exact result.
                    outer = {'id': 2, 'result': {'outer': 'preserved'}}
                    replies = [{'id': i, 'result': {}} for i in range(3, 8)]
                    sequence = pauses + ([outer] + replies if outer_first else replies + [outer])
                    client.receive = mock.Mock(side_effect=sequence)
                    client.sock.sendall.side_effect = lambda payload: releases.append(
                        (now[0], json.loads(payload)))
                    self.assertEqual(client.command('Runtime.evaluate', {'expression': 'original'}),
                                     {'outer': 'preserved'})
                    self.assertEqual(json.dumps(pauses), original)
                    self.assertEqual(releases[0][1], {'id': 2, 'method': 'Runtime.evaluate',
                                                     'params': {'expression': 'original'}})
                    self.assertEqual([t for t, _ in releases[1:]], [0, .25, .5, .75, 1])
                    self.assertEqual([m for _, m in releases[1:]], [
                        {'id': i + 3, 'method': 'Fetch.continueRequest',
                         'params': {'requestId': str(i)}} for i in range(5)])
                    self.assertEqual(client.next_id, 8)
                    self.assertEqual(client.receive.call_count, len(sequence))

    def test_disposed_fixture_favicon_ack_requires_exact_old_document(self):
        for late_navigation in (False, True):
            client = self.paced_client()
            paused = self.paused(resource='Other', url='http://fixture:8450/favicon.ico')
            paused['params'].update(networkId='icon', frameId='frame')
            start = {'method': 'Network.requestWillBeSent', 'params': {
                'requestId': 'icon', 'frameId': 'frame', 'loaderId': 'old-document',
                'type': 'Other', 'request': {'url': 'http://fixture:8450/favicon.ico', 'method': 'GET'}}}
            navigation = {'method': 'Page.frameNavigated', 'params': {
                'frame': {'id': 'frame', 'loaderId': 'new-document'}}}
            ack = {'id': 3, 'error': {'code': -32602, 'message': 'Invalid InterceptionId.'}}
            client.receive = mock.Mock(side_effect=[start, paused] +
                ([ack, navigation] if late_navigation else [navigation, ack]) +
                [{'id': 2, 'result': {'preserved': True}}])
            self.assertEqual(client.command('test'), {'preserved': True})
            self.assertEqual(client.sock.sendall.call_count, 2)

    def test_favicon_disposal_cannot_admit_application_or_unbound_requests(self):
        import copy
        for mutation in ('origin', 'path', 'query', 'type', 'frame', 'same-loader',
                         'no-loader', 'no-network', 'no-navigation', 'finished', 'failed', 'restart'):
            with self.subTest(mutation=mutation):
                client = self.paced_client()
                paused = self.paused(resource='Other', url='http://fixture:8450/favicon.ico')
                paused['params'].update(networkId='icon', frameId='frame')
                start = {'method': 'Network.requestWillBeSent', 'params': {
                    'requestId': 'icon', 'frameId': 'frame', 'loaderId': 'old',
                    'type': 'Other', 'request': {'url': 'http://fixture:8450/favicon.ico', 'method': 'GET'}}}
                navigation = {'method': 'Page.frameNavigated', 'params': {'frame': {'id': 'frame', 'loaderId': 'new'}}}
                if mutation == 'origin': start['params']['request']['url'] = 'http://other:8450/favicon.ico'
                if mutation == 'path': start['params']['request']['url'] = 'http://fixture:8450/console/loops/run'
                if mutation == 'query': start['params']['request']['url'] += '?extra=1'
                if mutation == 'type': start['params']['type'] = 'XHR'
                if mutation == 'frame': navigation['params']['frame']['id'] = 'other'
                if mutation == 'same-loader': navigation['params']['frame']['loaderId'] = 'old'
                if mutation == 'no-loader': start['params']['loaderId'] = ''
                if mutation == 'no-network': paused['params'].pop('networkId')
                events = [start, paused] + ([] if mutation == 'no-navigation' else [navigation])
                if mutation in ('finished', 'failed'):
                    events.append({'method': 'Network.loadingFinished' if mutation == 'finished' else 'Network.loadingFailed', 'params': {'requestId': 'icon'}})
                if mutation == 'restart': events.append(copy.deepcopy(start))
                client.events = events
                self.assertFalse(client._fixture_favicon_superseded(paused['params']))

    def test_network_start_after_pause_requires_first_exact_request_and_cancel(self):
        import copy
        for mutation in ('valid', 'url', 'method', 'frame', 'no-loader', 'no-cancel', 'success', 'restart'):
            with self.subTest(mutation=mutation):
                client = self.paced_client()
                paused = self.paused()
                paused['params'].update(networkId='network', frameId='frame')
                start = {'method': 'Network.requestWillBeSent', 'params': {
                    'requestId': 'network', 'frameId': 'frame', 'loaderId': 'loader',
                    'request': {'url': paused['params']['request']['url'], 'method': 'GET'}}}
                cancel = {'method': 'Network.loadingFailed', 'params': {
                    'requestId': 'network', 'canceled': True, 'errorText': 'net::ERR_ABORTED'}}
                if mutation == 'url': start['params']['request']['url'] += '-other'
                if mutation == 'method': start['params']['request']['method'] = 'POST'
                if mutation == 'frame': start['params']['frameId'] = 'other'
                if mutation == 'no-loader': start['params']['loaderId'] = ''
                if mutation == 'no-cancel': cancel['params']['canceled'] = False
                if mutation == 'success': cancel['method'] = 'Network.loadingFinished'
                client.events = [paused, start, cancel]
                if mutation == 'restart': client.events.extend([copy.deepcopy(start), cancel])
                self.assertEqual(client._fixture_request_cancelled(paused['params']), mutation == 'valid')
        client = self.paced_client()
        client.receive = mock.Mock(side_effect=[paused, {'id': 3, 'error': {
            'code': -32602, 'message': 'Invalid InterceptionId.'}}, {'id': 2, 'result': {'preserved': True}},
            start, cancel])
        self.assertEqual(client.command('test'), {'preserved': True})

    def test_pacing_has_no_idle_catchup_and_only_exact_origin(self):
        now = [0.0]
        sent = []
        with (mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]),
              mock.patch.object(browser.time, 'sleep', side_effect=lambda delay: now.__setitem__(0, now[0] + delay))):
            client = self.paced_client()
            now[0] = 10.0
            client.sock.sendall.side_effect = lambda payload: sent.append((now[0], json.loads(payload)))
            client.receive = mock.Mock(side_effect=[
                self.paused('a'), {'id': 3, 'result': {}},
                self.paused('external', url='http://fixture:84501/console'), {'id': 4, 'result': {}},
                self.paused('b'), {'id': 5, 'result': {}}, {'id': 2, 'result': {}}])
            client.command('test')
        self.assertEqual([t for t, m in sent if m['method'] == 'Fetch.continueRequest'],
                         [10.0, 10.0, 10.25])

    def test_release_schedule_persists_across_commands_and_idle(self):
        now = [0.0]
        sent = []
        with (mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]),
              mock.patch.object(browser.time, 'sleep', side_effect=lambda delay: now.__setitem__(0, now[0] + delay))):
            client = self.paced_client()
            client.sock.sendall.side_effect = lambda payload: sent.append((now[0], json.loads(payload)))
            for index in range(4):
                if index == 2:
                    now[0] = 10.0
                outer = client.next_id
                client.receive = mock.Mock(side_effect=[self.paused(str(index)),
                    {'id': outer, 'result': {'index': index}}, {'id': outer + 1, 'result': {}}])
                self.assertEqual(client.command('test'), {'index': index})
        self.assertEqual([t for t, m in sent if m['method'] == 'Fetch.continueRequest'],
                         [0.0, .25, 10.0, 10.25])
        self.assertEqual([m['id'] for _, m in sent], list(range(2, 10)))

    def test_pending_continuation_retains_deadline_after_outer_response(self):
        now = [0.0]
        with (mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]),
              mock.patch.object(browser, 'CDP_TIMEOUT', .1)):
            client = self.paced_client()
            messages = iter([self.paused(), {'id': 2, 'result': {'outer': True}},
                             {'id': 3, 'result': {}}])
            def receive():
                message = next(messages)
                if message.get('id') == 3:
                    now[0] = .11
                return message
            client.receive = mock.Mock(side_effect=receive)
            with self.assertRaisesRegex(RuntimeError, 'absolute deadline'):
                client.command('test')
            self.assertEqual(client.deadline, .1)

    def test_fetch_enable_error_is_not_ignored(self):
        client = self.client()
        client.receive = mock.Mock(return_value={'id': 1, 'error': {'message': 'denied'}})
        with self.assertRaisesRegex(RuntimeError, 'Fetch.enable failed'):
            client.pace_fixture_requests('http://fixture:8450')

    def test_unpaced_client_leaves_fetch_events_unchanged(self):
        client = self.client()
        client.receive = mock.Mock(side_effect=[self.paused(), {'id': 1, 'result': {}}])
        with mock.patch.object(browser.time, 'sleep') as sleep:
            self.assertEqual(client.command('test'), {})
        self.assertEqual(client.sock.sendall.call_count, 1)
        sleep.assert_not_called()
        self.assertEqual(client.events, [self.paused()])

    def test_continuation_errors_fail_closed_in_both_reply_orders(self):
        for outer_first in (False, True):
            with self.subTest(outer_first=outer_first):
                client = self.paced_client()
                replies = [{'id': 2, 'result': {'saved': True}},
                           {'id': 3, 'error': {'message': 'private error'}}]
                if not outer_first:
                    replies.reverse()
                client.receive = mock.Mock(side_effect=[self.paused()] + replies)
                with self.assertRaisesRegex(RuntimeError, 'Fetch.continueRequest failed') as failure:
                    client.command('test')
                self.assertNotIn('private error', str(failure.exception))

    def test_exact_cancelled_continuation_ack_preserves_outer_reply(self):
        for outer_first in (False, True):
            for cancel_first in (False, True):
                with self.subTest(outer_first=outer_first, cancel_first=cancel_first):
                    client = self.paced_client()
                    paused = self.paused()
                    paused['params'].update(networkId='network', frameId='frame')
                    start = {'method': 'Network.requestWillBeSent', 'params': {
                        'requestId': 'network', 'frameId': 'frame', 'loaderId': 'loader'}}
                    cancel = {'method': 'Network.loadingFailed', 'params': {
                        'requestId': 'network', 'canceled': True, 'errorText': 'net::ERR_ABORTED'}}
                    outer = {'id': 2, 'result': {'preserved': True}}
                    ack = {'id': 3, 'error': {'code': -32602, 'message': 'Invalid InterceptionId.'}}
                    pair = [cancel, ack] if cancel_first else [ack, cancel]
                    sequence = [start, paused] + ([outer] + pair if outer_first else pair + [outer])
                    client.receive = mock.Mock(side_effect=sequence)
                    self.assertEqual(client.command('test'), {'preserved': True})
                    self.assertEqual(client.sock.sendall.call_count, 2)  # No retry/second ack.
                    self.assertEqual(client.receive.call_count, len(sequence))

    def test_invalid_interception_without_exact_cancellation_fails_closed(self):
        for mutation in ('network', 'frame', 'loader', 'canceled', 'reason', 'code',
                         'message', 'missing-network', 'finished', 'redirect-reuse', 'replaced-loader'):
            with self.subTest(mutation=mutation):
                client = self.paced_client()
                paused = self.paused()
                paused['params'].update(networkId='network', frameId='frame')
                start = {'method': 'Network.requestWillBeSent', 'params': {
                    'requestId': 'network', 'frameId': 'frame', 'loaderId': 'loader'}}
                cancel = {'method': 'Network.loadingFailed', 'params': {
                    'requestId': 'network', 'canceled': True, 'errorText': 'net::ERR_ABORTED'}}
                error = {'code': -32602, 'message': 'Invalid InterceptionId.'}
                extra = []
                if mutation == 'network': cancel['params']['requestId'] = 'other'
                if mutation == 'frame': start['params']['frameId'] = 'other'
                if mutation == 'loader': start['params']['loaderId'] = ''
                if mutation == 'canceled': cancel['params']['canceled'] = False
                if mutation == 'reason': cancel['params']['errorText'] = 'private error'
                if mutation == 'code': error['code'] = -32000
                if mutation == 'message': error['message'] = 'private error'
                if mutation == 'missing-network': paused['params'].pop('networkId')
                if mutation == 'finished': extra = [{'method': 'Network.loadingFinished', 'params': {'requestId': 'network'}}]
                if mutation == 'redirect-reuse': extra = [start]
                if mutation == 'replaced-loader':
                    extra = [{'method': 'Network.requestWillBeSent', 'params': {
                        'requestId': 'network', 'frameId': 'frame', 'loaderId': 'new-loader'}}, cancel]
                sequence = [start, paused, cancel] + extra + [
                    {'id': 2, 'result': {}}, {'id': 3, 'error': error}]
                now = [0.0]
                client.fixture_next_release = 0.0
                messages = iter(sequence)
                def receive():
                    message = next(messages, None)
                    if message is None:
                        now[0] = browser.CDP_TIMEOUT
                        raise TimeoutError()
                    return message
                client.receive = mock.Mock(side_effect=receive)
                with mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]):
                    with self.assertRaisesRegex(RuntimeError, 'Fetch.continueRequest failed') as failure:
                        client.command('test')
                self.assertNotIn('private', str(failure.exception))
                self.assertIn('code=', str(failure.exception))
                self.assertEqual(client.sock.sendall.call_count, 2)

    def test_deferred_ack_keeps_original_deadline_and_safe_context(self):
        for late_cancel in (False, True):
            with self.subTest(late_cancel=late_cancel):
                now = [0.0]
                client = self.paced_client()
                paused = self.paused(url='https://user:private-pass@fixture:8450/console?private-query#private-fragment')
                paused['params'].update(networkId='private-network', frameId='private-frame')
                sequence = iter([
                    {'method': 'Network.requestWillBeSent', 'params': {
                        'requestId': 'private-network', 'frameId': 'private-frame', 'loaderId': 'private-loader'}},
                    paused, {'id': 3, 'error': {'code': -32602, 'message': 'Invalid InterceptionId.'}},
                    {'id': 2, 'result': {'preserved': True}},
                ])
                def receive():
                    event = next(sequence, None)
                    if event is None:
                        now[0] = .1
                        if late_cancel:
                            return {'method': 'Network.loadingFailed', 'params': {
                                'requestId': 'private-network', 'canceled': True, 'errorText': 'net::ERR_ABORTED'}}
                        raise TimeoutError()
                    return event
                client.receive = mock.Mock(side_effect=receive)
                with (mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]),
                      mock.patch.object(browser, 'CDP_TIMEOUT', .1)):
                    with self.assertRaisesRegex(RuntimeError, 'cancellation_unverified') as failure:
                        client.command('test')
                self.assertEqual(client.deadline, .1)
                self.assertEqual(client.sock.sendall.call_count, 2)
                message = str(failure.exception)
                for forbidden in ('private', 'user', 'headers', 'body', '?', '#'):
                    self.assertNotIn(forbidden, message)
                self.assertIn('"path": "/console"', message)
                self.assertIn('"start_seen": true', message)
                self.assertIn('"exact_cancelled": false', message)

    def test_delayed_cancellation_context_denials_in_both_reply_orders(self):
        for outer_first in (False, True):
            for mutation in ('network', 'frame', 'loader', 'finished', 'missing-frame', 'missing-start'):
                with self.subTest(outer_first=outer_first, mutation=mutation):
                    now = [0.0]
                    with mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]):
                        client = self.paced_client()
                        paused = self.paused()
                        paused['params'].update(networkId='network', frameId='frame')
                        start = {'method': 'Network.requestWillBeSent', 'params': {
                            'requestId': 'network', 'frameId': 'frame', 'loaderId': 'loader'}}
                        cancel = {'method': 'Network.loadingFailed', 'params': {
                            'requestId': 'network', 'canceled': True, 'errorText': 'net::ERR_ABORTED'}}
                        if mutation == 'network': cancel['params']['requestId'] = 'other'
                        if mutation == 'frame': start['params']['frameId'] = 'other'
                        if mutation == 'loader': start['params']['loaderId'] = ''
                        if mutation == 'finished': cancel['method'] = 'Network.loadingFinished'
                        if mutation == 'missing-frame': paused['params'].pop('frameId')
                        outer = {'id': 2, 'result': {}}
                        ack = {'id': 3, 'error': {'code': -32602, 'message': 'Invalid InterceptionId.'}}
                        sequence = iter(([] if mutation == 'missing-start' else [start]) + [paused]
                                        + ([outer, ack, cancel] if outer_first else [ack, cancel, outer]))
                        def receive():
                            message = next(sequence, None)
                            if message is None:
                                now[0] = browser.CDP_TIMEOUT
                                raise TimeoutError()
                            return message
                        client.receive = mock.Mock(side_effect=receive)
                        with self.assertRaisesRegex(RuntimeError, 'cancellation_unverified'):
                            client.command('test')
                        self.assertEqual(client.deadline, browser.CDP_TIMEOUT)
                        self.assertEqual(client.sock.sendall.call_count, 2)

    def test_deferred_ack_still_consumes_original_event_budget(self):
        client = self.paced_client()
        client.receive = mock.Mock(side_effect=[self.paused(),
            {'id': 3, 'error': {'code': -32602, 'message': 'Invalid InterceptionId.'}},
            {'id': 2, 'result': {}}, {'method': 'noise'}])
        with mock.patch.object(browser, 'MAX_EVENTS', 2):
            with self.assertRaisesRegex(RuntimeError, 'event budget'):
                client.command('test')

    def test_pause_before_network_start_is_not_loader_proof(self):
        client = self.paced_client()
        paused = self.paused()
        paused['params'].update(networkId='network', frameId='frame')
        client.events = [paused,
            {'method': 'Network.requestWillBeSent', 'params': {
                'requestId': 'network', 'frameId': 'frame', 'loaderId': 'loader'}},
            {'method': 'Network.loadingFailed', 'params': {
                'requestId': 'network', 'canceled': True, 'errorText': 'net::ERR_ABORTED'}}]
        self.assertFalse(client._fixture_request_cancelled(paused['params']))

    def test_outer_completion_does_not_wait_for_future_pauses(self):
        client = self.paced_client()
        client.receive = mock.Mock(side_effect=[{'id': 2, 'result': {}}, self.paused()])
        self.assertEqual(client.command('test'), {})
        self.assertEqual(client.receive.call_count, 1)
        self.assertEqual(client.sock.sendall.call_count, 1)

    def test_pacing_does_not_extend_absolute_deadline(self):
        now = [0.0]
        with (mock.patch.object(browser.time, 'monotonic', side_effect=lambda: now[0]),
              mock.patch.object(browser.time, 'sleep', side_effect=lambda delay: now.__setitem__(0, now[0] + delay)),
              mock.patch.object(browser, 'CDP_TIMEOUT', .1)):
            client = self.paced_client()
            client.receive = mock.Mock(side_effect=[self.paused('a'), {'id': 3, 'result': {}},
                                                   self.paused('b')])
            with self.assertRaisesRegex(RuntimeError, 'absolute deadline'):
                client.command('test')
            self.assertEqual(client.deadline, .1)
            self.assertEqual(now[0], .1)
            self.assertEqual(client.sock.sendall.call_count, 2)

    def test_paced_events_and_continuation_replies_keep_resource_budgets(self):
        for limit, value in (('MAX_EVENTS', 0), ('MAX_EVENT_BYTES', 1), ('MAX_EVENTS', 1)):
            with self.subTest(limit=limit, value=value):
                client = self.paced_client()
                client.receive = mock.Mock(side_effect=[self.paused(), {'id': 3, 'result': {}},
                                                       {'id': 2, 'result': {}}])
                with mock.patch.object(browser, limit, value):
                    with self.assertRaisesRegex(RuntimeError, 'event budget'):
                        client.command('test')

    def test_continuation_still_uses_frame_budget(self):
        client = self.paced_client()
        del client._frame
        client.receive = mock.Mock(side_effect=[self.paused('x' * (MAX_FRAME_BYTES + 1))])
        with self.assertRaisesRegex(RuntimeError, 'output exceeds'):
            client.command('test')

    def test_fixture_instrumentation_precedes_first_origin_navigation(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            root = Path(directory)
            passwords = root / 'passwords.json'
            passwords.write_text(json.dumps({'initial': 'initial-fixture-only',
                                            'replacement': 'replacement-fixture-only'}))
            client = mock.Mock()
            order = mock.Mock()
            order.attach_mock(client.pace_fixture_requests, 'pace')
            navigate = mock.Mock(side_effect=RuntimeError('stop before journey'))
            order.attach_mock(navigate, 'navigate')
            with (mock.patch.object(browser.sys, 'argv', ['browser', 'http://fixture:8450', str(passwords), str(root)]),
                  mock.patch.object(browser.subprocess, 'Popen') as popen,
                  mock.patch.object(browser, 'wait_for_active_port', return_value=123),
                  mock.patch.object(browser, 'page_websocket', return_value='ws://fixture'),
                  mock.patch.object(browser, 'DevTools', return_value=client),
                  mock.patch.object(browser, 'navigate', navigate),
                  mock.patch.object(browser, 'stop_chrome')):
                with self.assertRaisesRegex(RuntimeError, 'stop before journey'):
                    browser.main()
            self.assertEqual(popen.call_args.args[0][-1], 'about:blank')
            self.assertEqual(order.mock_calls, [mock.call.pace('http://fixture:8450'),
                                               mock.call.navigate(client, 'http://fixture:8450/console')])
            self.assertEqual([call.args[0] for call in client.command.call_args_list],
                             ['Page.enable', 'Runtime.enable', 'Log.enable', 'Network.enable', 'Audits.enable'])

    def test_429_is_sanitized_and_still_fails_without_console_log(self):
        params = {'type': 'Stylesheet', 'response': {'status': 429,
                  'url': 'https://user:private-pass@fixture:8450/assets/style.css?private-query#private-fragment',
                  'headers': {'Cookie': 'private-cookie'}, 'body': 'private-body'}}
        with self.assertRaisesRegex(RuntimeError, 'unexpected HTTP 429') as failure:
            browser.validate_browser_events([{'method': 'Network.responseReceived', 'params': params}], -1, -1)
        message = str(failure.exception)
        for forbidden in ('user', 'private-', 'headers', 'body', '?', '#'):
            self.assertNotIn(forbidden, message)
        self.assertIn('"host": "fixture"', message)
        self.assertIn('"scheme": "https"', message)
        self.assertIn('"path": "/assets/style.css"', message)
        self.assertIn('"type": "Stylesheet"', message)
        for event in ({'method': 'Log.entryAdded', 'params': {'entry': {'level': 'error', 'text': 'error'}}},
                      {'method': 'Audits.issueAdded', 'params': {'issue': {'code': 'ContentSecurityPolicyIssue'}}},
                      {'method': 'Runtime.exceptionThrown'}):
            with self.subTest(event=event), self.assertRaises(RuntimeError):
                browser.validate_browser_events([event], -1, -1)

    def test_child_output_bounded(self):
        with self.assertRaisesRegex(RuntimeError, 'output exceeds'):
            run_owned([sys.executable, '-c', 'print("x"*100000)'], timeout=3, max_output=100)

    def test_timeout_kills_grandchild(self):
        with tempfile.TemporaryDirectory(dir=Path.cwd()) as directory:
            marker = Path(directory)/'child'
            code = 'import subprocess,time,pathlib; p=subprocess.Popen(["sleep","30"]); pathlib.Path('+repr(str(marker))+').write_text(str(p.pid)); time.sleep(30)'
            with self.assertRaises(subprocess.TimeoutExpired):
                run_owned([sys.executable, '-c', code], timeout=.5)
            pid = int(marker.read_text())
            # A killed orphan may be a zombie until the system reaper collects it.
            for _ in range(50):
                status = Path(f'/proc/{pid}/stat')
                if process_exited(status):
                    break
                time.sleep(.02)
            else:
                self.fail('owned grandchild survived timeout')

    def test_reaped_grandchild_missing_stat_is_exited(self):
        status = mock.Mock()
        status.read_text.side_effect = FileNotFoundError
        self.assertTrue(process_exited(status))

    def test_reaped_grandchild_process_lookup_is_exited(self):
        status = mock.Mock()
        status.read_text.side_effect = ProcessLookupError(3, 'No such process')
        self.assertTrue(process_exited(status))

    def test_grandchild_exit_check_preserves_live_and_error_denials(self):
        status = mock.Mock()
        status.read_text.return_value = '123 (sleep) S'
        self.assertFalse(process_exited(status))
        status.read_text.return_value = '123 (sleep) Z'
        self.assertTrue(process_exited(status))
        status.read_text.side_effect = PermissionError
        with self.assertRaises(PermissionError):
            process_exited(status)

    def test_supervisor_no_unbounded_fallback(self):
        spec = importlib.util.spec_from_file_location('budget', Path(__file__).with_name('verify-budget.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with mock.patch.object(module.shutil, 'which', return_value=None):
            with self.assertRaisesRegex(RuntimeError, 'no unbounded fallback'):
                module.command(['true'])
        with (mock.patch.object(module.sys, 'platform', 'linux'),
              mock.patch.object(module.Path, 'exists', return_value=True),
              mock.patch.object(module.shutil, 'which', return_value='/fixture/systemd-run')):
            for environment, expected in (({}, '512MiB'), ({'GOMEMLIMIT': '384MiB'}, '384MiB')):
                with mock.patch.dict(module.os.environ, environment, clear=True):
                    unit, command = module.command(['true'])
                self.assertIn('--setenv=GOMEMLIMIT=' + expected, command)
                self.assertEqual(sum(arg.startswith('--setenv=GOMEMLIMIT=') for arg in command), 1)
        for prop in ('MemoryMax=6G', 'MemorySwapMax=0', 'TasksMax=256', 'RuntimeMaxSec=3600', 'KillMode=control-group'):
            self.assertIn('--property='+prop, command)


if __name__ == '__main__':
    unittest.main()

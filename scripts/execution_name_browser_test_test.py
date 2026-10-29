"""Regression coverage for the execution-name proof's document/layout barrier."""
import contextlib
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

from scripts import console_browser_test

with mock.patch.dict(sys.modules, {'console_browser_test': console_browser_test}):
    from scripts import execution_name_browser_test as proof


class RenderReadinessTest(unittest.TestCase):
    def test_same_url_stale_document_is_rejected_before_render_wait(self):
        browser = mock.Mock()
        # The old document already has the right URL and selector; only its
        # identity distinguishes it from the newly loaded complete document.
        browser.evaluate.side_effect = [123.5, False, False, True]
        browser.command.return_value = {'result': {'value': True}}
        with mock.patch.object(console_browser_test.time, 'sleep'):
            proof.navigate_ready(browser, 'http://localhost/console/queue')
        calls = browser.mock_calls
        self.assertEqual(calls[0], mock.call.evaluate('performance.timeOrigin'))
        self.assertEqual(calls[1], mock.call.command('Page.navigate', {
            'url': 'http://localhost/console/queue'}))
        expressions = [c.args[0] for c in calls if c[0] == 'evaluate'][1:]
        self.assertEqual(len(expressions), 3)
        for expression in expressions:
            self.assertIn('performance.timeOrigin !== 123.5', expression)
            self.assertIn('location.href === "http://localhost/console/queue"', expression)
            self.assertIn('document.readyState === "complete"', expression)
        self.assertEqual(calls[-1].args[0], 'Runtime.evaluate')
        params = calls[-1].args[1]
        self.assertTrue(params['awaitPromise'])
        expression = params['expression']
        self.assertLess(expression.index('await document.fonts.ready'),
                        expression.index('await new Promise(requestAnimationFrame)'))
        self.assertEqual(expression.count('await new Promise(requestAnimationFrame)'), 2)

    def test_failed_document_barrier_does_not_start_layout_wait(self):
        browser = mock.Mock()
        browser.evaluate.return_value = 123.5
        browser.command.return_value = {}
        with mock.patch.object(console_browser_test, 'wait_for', side_effect=RuntimeError('stale')):
            with self.assertRaisesRegex(RuntimeError, 'stale'):
                proof.navigate_ready(browser, 'http://localhost/console/queue')
        browser.command.assert_called_once_with('Page.navigate', {'url': 'http://localhost/console/queue'})

    def test_render_exception_fails_closed(self):
        browser = mock.Mock()
        browser.evaluate.return_value = 123.5
        browser.command.return_value = {'exceptionDetails': {'text': 'fixture failure'}}
        with mock.patch.object(console_browser_test, 'navigate'), mock.patch.object(console_browser_test, 'wait_for'):
            with self.assertRaisesRegex(RuntimeError, 'render readiness failed'):
                proof.navigate_ready(browser, 'http://localhost/console/queue')

    def test_offending_geometry_is_persisted_and_printed(self):
        value = {'left': -5, 'right': 999, 'width': 390, 'injection': False}
        with tempfile.TemporaryDirectory() as directory, contextlib.redirect_stdout(io.StringIO()) as output:
            results = []
            proof.record_geometry(Path(directory), results, value)
            self.assertEqual(json.loads((Path(directory) / 'browser-results.json').read_text()), [value])
            self.assertIn('"left": -5', output.getvalue())
            self.assertIn('"right": 999', output.getvalue())


if __name__ == '__main__':
    unittest.main()

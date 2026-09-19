#!/bin/sh
# Wrapper invoked by $(SHELL) in the Aegis Makefile. Normalizes umask to
# the value CI uses so socket/mode-sensitive tests observe the same
# permissions they would on a clean ubuntu-latest runner. Without this,
# host-side umask 0002 lets tests race and observe a wrong initial mode
# before the server rechmods the socket.
umask 0022
if [ -n "${AEGIS_VERIFY_UNIT:-}" ]; then
    exec python3 scripts/verify-budget-stage.py "$@"
fi
exec /bin/sh "$@"
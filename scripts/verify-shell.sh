#!/bin/sh
# Wrapper invoked by $(SHELL) in the Aegis Makefile. Normalizes umask to
# the value CI uses so socket/mode-sensitive tests observe the same
# permissions they would on a clean ubuntu-latest runner. Without this,
# host-side umask 0002 lets tests race and observe a wrong initial mode
# before the server rechmods the socket.
umask 0022
exec /bin/sh "$@"
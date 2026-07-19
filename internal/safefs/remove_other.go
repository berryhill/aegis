//go:build !linux

package safefs

import "errors"

var errUnsupported = errors.New("descriptor-anchored deletion is unavailable on this platform; no mutation performed")

func EmptyDirectory(string) error                                       { return errUnsupported }
func EmptyDirectoryIdentity(string, uint64, uint64) error               { return errUnsupported }
func RemoveRelative(string, string, bool) error                         { return errUnsupported }
func RemoveRelativeIdentity(string, string, bool, uint64, uint64) error { return errUnsupported }
func RemoveFile(string, string) error                                   { return errUnsupported }

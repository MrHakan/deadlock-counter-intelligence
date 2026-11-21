//go:build !linux && !windows

package main

import "errors"

func enterRaw() (func(), error)                    { return nil, errors.New("not supported") }
func terminalSize() (int, int)                     { return 80, 24 }
func colorSupported() bool                         { return false }
func deadlockRunning() bool                        { return false }
func registryValue(root, path, name string) string { return "" }

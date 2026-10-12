// Copyright (c) 2017, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package interp

import (
	"strconv"
	"testing"
	"time"
)

func TestElapsedString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in    time.Duration
		posix bool
		want  string
	}{
		{time.Nanosecond, false, "0m0.000s"},
		{time.Millisecond, false, "0m0.001s"},
		{time.Millisecond, true, "0.00"},
		{2500 * time.Millisecond, false, "0m2.500s"},
		{2500 * time.Millisecond, true, "2.50"},
		{
			10*time.Minute + 10*time.Second,
			false,
			"10m10.000s",
		},
		{
			10*time.Minute + 10*time.Second,
			true,
			"610.00",
		},
		{31 * time.Second, false, "0m31.000s"},
		{102 * time.Second, false, "1m42.000s"},
	}
	for _, tc := range tests {
		t.Run(tc.in.String(), func(t *testing.T) {
			got := elapsedString(tc.in, tc.posix)
			if got != tc.want {
				t.Fatalf("wanted %q, got %q", tc.want, got)
			}
		})
	}
}

// Signal numbers are not portable and the differences are not obscure: USR1 is
// 10 on Linux and 30 on darwin, where 10 is BUS. These tests are written
// entirely in terms of names for that reason — they assert the same thing on
// every platform, and they fail on darwin against a hardcoded Linux table
// even though they can only be run here.
func TestSignalNamesRoundTripThroughThePlatform(t *testing.T) {
	t.Parallel()
	// Signals every unix has, by name. No number appears anywhere below.
	for _, name := range []string{
		"HUP", "INT", "QUIT", "ILL", "ABRT", "FPE", "KILL", "SEGV",
		"PIPE", "ALRM", "TERM", "USR1", "USR2", "CHLD", "CONT", "STOP",
	} {
		num, canon, ok := signalByName(name)
		if !ok {
			t.Errorf("%s: not resolved on this platform", name)
			continue
		}
		if canon != name {
			t.Errorf("%s: canonical name came back as %q", name, canon)
		}
		if num <= 0 {
			t.Errorf("%s: resolved to %d", name, num)
			continue
		}
		// The number must name the same signal going back the other way,
		// which is what fails when one direction reads a stale table.
		if back := signalName(num); back != name {
			t.Errorf("%s resolved to %d, which is %q going back", name, num, back)
		}
		// SIG-prefixed and numeric spellings must agree with the bare name.
		if n, _, ok := signalByName("SIG" + name); !ok || n != num {
			t.Errorf("SIG%s resolved to %d, want %d", name, n, num)
		}
		if n, c, ok := signalByName(strconv.Itoa(num)); !ok || n != num || c != name {
			t.Errorf("%d resolved to (%d, %q), want (%d, %q)", num, n, c, num, name)
		}
	}
}

func TestEffectOfIsKeyedOnNamesNotNumbers(t *testing.T) {
	t.Parallel()
	// Job control needs a terminal, so these cannot act on a goroutine.
	// effectOf used to switch on 17 through 22, which is this set on Linux
	// and a different one on darwin, where 17 is STOP and 20 is CHLD.
	for _, name := range []string{"CHLD", "CONT", "STOP", "TSTP", "TTIN", "TTOU"} {
		num, _, ok := signalByName(name)
		if !ok {
			continue // not every platform has all of them
		}
		if got := effectOf(num); got != signalUnsupported {
			t.Errorf("%s (%d): effect %v, want signalUnsupported", name, num, got)
		}
	}
	for _, name := range []string{"HUP", "INT", "KILL", "TERM", "USR1", "USR2"} {
		num, _, ok := signalByName(name)
		if !ok {
			continue
		}
		if got := effectOf(num); got != signalTerminates {
			t.Errorf("%s (%d): effect %v, want signalTerminates", name, num, got)
		}
	}
	if got := effectOf(0); got != signalTests {
		t.Errorf("signal 0: effect %v, want signalTests", got)
	}
}

func TestUnknownSignalsAreRejected(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{"NOSUCHSIG", "SIGNOSUCHSIG", "-1", "9999", ""} {
		if _, _, ok := signalByName(spec); ok {
			t.Errorf("%q was accepted as a signal", spec)
		}
	}
}

package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DougNorm/optimism/cannon/mipsevm"
)

type StepMatcher func(st *mipsevm.State) bool

type StepMatcherFlag struct {
	repr    string
	matcher StepMatcher
}

func MustStepMatcherFlag(pattern string) *StepMatcherFlag {
	out := new(StepMatcherFlag)
	if err := out.Set(pattern); err != nil {
		panic(err)
	}
	return out
}

func (m *StepMatcherFlag) Set(value string) error {
	m.repr = value
	if value == "" || value == "never" {
		m.matcher = func(st *mipsevm.State) bool {
			return false
		}
	} else if value == "always" {
		m.matcher = func(st *mipsevm.State) bool {
			return true
		}
	} else if strings.HasPrefix(value, "=") {
		when, err := strconv.ParseUint(value[1:], 0, 64)
		if err != nil {
			return fmt.Errorf("failed to parse step number: %w", err)
		}
		m.matcher = func(st *mipsevm.State) bool {
			return st.Step == when
		}
	} else if strings.HasPrefix(value, "%") {
		when, err := strconv.ParseUint(value[1:], 0, 64)
		if err != nil {
			return fmt.Errorf("failed to parse step interval number: %w", err)
		}
		m.matcher = func(st *mipsevm.State) bool {
			return st.Step%when == 0
		}
	} else {
		return fmt.Errorf("unrecognized step matcher: %q", value)
	}
	return nil
}

func (m *StepMatcherFlag) String() string {
	return m.repr
}

func (m *StepMatcherFlag) Matcher() StepMatcher {
	if m.matcher == nil { // Set(value) is not called for omitted inputs, default to never matching.
		return func(st *mipsevm.State) bool {
			return false
		}
	}
	return m.matcher
}

func (m *StepMatcherFlag) Clone() any {
	var out StepMatcherFlag
	if err := out.Set(m.repr); err != nil {
		panic(fmt.Errorf("invalid repr: %w", err))
	}
	return &out
}

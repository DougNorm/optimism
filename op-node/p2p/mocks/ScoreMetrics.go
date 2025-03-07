// Code generated by mockery v2.28.1. DO NOT EDIT.

package mocks

import (
	mock "github.com/stretchr/testify/mock"

	store "github.com/DougNorm/optimism/op-node/p2p/store"
)

// ScoreMetrics is an autogenerated mock type for the ScoreMetrics type
type ScoreMetrics struct {
	mock.Mock
}

// SetPeerScores provides a mock function with given fields: _a0
func (_m *ScoreMetrics) SetPeerScores(_a0 []store.PeerScores) {
	_m.Called(_a0)
}

type mockConstructorTestingTNewScoreMetrics interface {
	mock.TestingT
	Cleanup(func())
}

// NewScoreMetrics creates a new instance of ScoreMetrics. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewScoreMetrics(t mockConstructorTestingTNewScoreMetrics) *ScoreMetrics {
	mock := &ScoreMetrics{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

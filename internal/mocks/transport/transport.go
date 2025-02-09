// Code generated by MockGen. DO NOT EDIT.
// Source: types.go

// Package transportmock is a generated GoMock package.
package transportmock

import (
	context "context"
	io "io"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	raftpb "go.linka.cloud/raft/internal/raftpb"
	transport "go.linka.cloud/raft/internal/transport"
	raftlog "go.linka.cloud/raft/raftlog"
	raftpb0 "go.etcd.io/etcd/raft/v3/raftpb"
)

// MockConfig is a mock of Config interface.
type MockConfig struct {
	ctrl     *gomock.Controller
	recorder *MockConfigMockRecorder
}

// MockConfigMockRecorder is the mock recorder for MockConfig.
type MockConfigMockRecorder struct {
	mock *MockConfig
}

// NewMockConfig creates a new mock instance.
func NewMockConfig(ctrl *gomock.Controller) *MockConfig {
	mock := &MockConfig{ctrl: ctrl}
	mock.recorder = &MockConfigMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockConfig) EXPECT() *MockConfigMockRecorder {
	return m.recorder
}

// Controller mocks base method.
func (m *MockConfig) Controller() transport.Controller {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Controller")
	ret0, _ := ret[0].(transport.Controller)
	return ret0
}

// Controller indicates an expected call of Controller.
func (mr *MockConfigMockRecorder) Controller() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Controller", reflect.TypeOf((*MockConfig)(nil).Controller))
}

// GroupID mocks base method.
func (m *MockConfig) GroupID() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GroupID")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// GroupID indicates an expected call of GroupID.
func (mr *MockConfigMockRecorder) GroupID() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GroupID", reflect.TypeOf((*MockConfig)(nil).GroupID))
}

// Logger mocks base method.
func (m *MockConfig) Logger() raftlog.Logger {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Logger")
	ret0, _ := ret[0].(raftlog.Logger)
	return ret0
}

// Logger indicates an expected call of Logger.
func (mr *MockConfigMockRecorder) Logger() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Logger", reflect.TypeOf((*MockConfig)(nil).Logger))
}

// MockHandler is a mock of Handler interface.
type MockHandler struct {
	ctrl     *gomock.Controller
	recorder *MockHandlerMockRecorder
}

// MockHandlerMockRecorder is the mock recorder for MockHandler.
type MockHandlerMockRecorder struct {
	mock *MockHandler
}

// NewMockHandler creates a new mock instance.
func NewMockHandler(ctrl *gomock.Controller) *MockHandler {
	mock := &MockHandler{ctrl: ctrl}
	mock.recorder = &MockHandlerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockHandler) EXPECT() *MockHandlerMockRecorder {
	return m.recorder
}

// MockClient is a mock of Client interface.
type MockClient struct {
	ctrl     *gomock.Controller
	recorder *MockClientMockRecorder
}

// MockClientMockRecorder is the mock recorder for MockClient.
type MockClientMockRecorder struct {
	mock *MockClient
}

// NewMockClient creates a new mock instance.
func NewMockClient(ctrl *gomock.Controller) *MockClient {
	mock := &MockClient{ctrl: ctrl}
	mock.recorder = &MockClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockClient) EXPECT() *MockClientMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockClient) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockClientMockRecorder) Close() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockClient)(nil).Close))
}

// Join mocks base method.
func (m *MockClient) Join(arg0 context.Context, arg1 raftpb.Member) (*raftpb.JoinResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Join", arg0, arg1)
	ret0, _ := ret[0].(*raftpb.JoinResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Join indicates an expected call of Join.
func (mr *MockClientMockRecorder) Join(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Join", reflect.TypeOf((*MockClient)(nil).Join), arg0, arg1)
}

// Message mocks base method.
func (m *MockClient) Message(arg0 context.Context, arg1 raftpb0.Message) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Message", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Message indicates an expected call of Message.
func (mr *MockClientMockRecorder) Message(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Message", reflect.TypeOf((*MockClient)(nil).Message), arg0, arg1)
}

// PromoteMember mocks base method.
func (m_2 *MockClient) PromoteMember(ctx context.Context, m raftpb.Member) error {
	m_2.ctrl.T.Helper()
	ret := m_2.ctrl.Call(m_2, "PromoteMember", ctx, m)
	ret0, _ := ret[0].(error)
	return ret0
}

// PromoteMember indicates an expected call of PromoteMember.
func (mr *MockClientMockRecorder) PromoteMember(ctx, m interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PromoteMember", reflect.TypeOf((*MockClient)(nil).PromoteMember), ctx, m)
}

// MockController is a mock of Controller interface.
type MockController struct {
	ctrl     *gomock.Controller
	recorder *MockControllerMockRecorder
}

// MockControllerMockRecorder is the mock recorder for MockController.
type MockControllerMockRecorder struct {
	mock *MockController
}

// NewMockController creates a new mock instance.
func NewMockController(ctrl *gomock.Controller) *MockController {
	mock := &MockController{ctrl: ctrl}
	mock.recorder = &MockControllerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockController) EXPECT() *MockControllerMockRecorder {
	return m.recorder
}

// Join mocks base method.
func (m *MockController) Join(arg0 context.Context, arg1 uint64, arg2 *raftpb.Member) (*raftpb.JoinResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Join", arg0, arg1, arg2)
	ret0, _ := ret[0].(*raftpb.JoinResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Join indicates an expected call of Join.
func (mr *MockControllerMockRecorder) Join(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Join", reflect.TypeOf((*MockController)(nil).Join), arg0, arg1, arg2)
}

// PromoteMember mocks base method.
func (m *MockController) PromoteMember(arg0 context.Context, arg1 uint64, arg2 raftpb.Member) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PromoteMember", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// PromoteMember indicates an expected call of PromoteMember.
func (mr *MockControllerMockRecorder) PromoteMember(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PromoteMember", reflect.TypeOf((*MockController)(nil).PromoteMember), arg0, arg1, arg2)
}

// Push mocks base method.
func (m *MockController) Push(arg0 context.Context, arg1 uint64, arg2 raftpb0.Message) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Push", arg0, arg1, arg2)
	ret0, _ := ret[0].(error)
	return ret0
}

// Push indicates an expected call of Push.
func (mr *MockControllerMockRecorder) Push(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Push", reflect.TypeOf((*MockController)(nil).Push), arg0, arg1, arg2)
}

// SnapshotReader mocks base method.
func (m *MockController) SnapshotReader(arg0, arg1, arg2 uint64) (io.ReadCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SnapshotReader", arg0, arg1, arg2)
	ret0, _ := ret[0].(io.ReadCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SnapshotReader indicates an expected call of SnapshotReader.
func (mr *MockControllerMockRecorder) SnapshotReader(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SnapshotReader", reflect.TypeOf((*MockController)(nil).SnapshotReader), arg0, arg1, arg2)
}

// SnapshotWriter mocks base method.
func (m *MockController) SnapshotWriter(arg0, arg1, arg2 uint64) (io.WriteCloser, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SnapshotWriter", arg0, arg1, arg2)
	ret0, _ := ret[0].(io.WriteCloser)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SnapshotWriter indicates an expected call of SnapshotWriter.
func (mr *MockControllerMockRecorder) SnapshotWriter(arg0, arg1, arg2 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SnapshotWriter", reflect.TypeOf((*MockController)(nil).SnapshotWriter), arg0, arg1, arg2)
}

package raft

//go:generate mockgen -package transportmock  -source internal/transport/types.go -destination internal/mocks/transport/transport.go
//go:generate mockgen -package storagemock -source internal/storage/types.go -destination internal/mocks/storage/storage.go
//go:generate mockgen -package raftengine  -source internal/raftengine/types.go -destination internal/raftengine/types_test.go
//go:generate mockgen -package raftenginemock  -source internal/raftengine/engine.go -destination internal/mocks/raftengine/engine.go
//go:generate mockgen -package raftengine  -source vendor/go.etcd.io/raft/v3/node.go -destination internal/raftengine/node_test.go
//go:generate mockgen -package membershipmock  -source internal/membership/types.go -destination internal/mocks/membership/membership.go
//go:generate mockgen -package membership  -source internal/membership/types.go -destination internal/membership/types_test.go

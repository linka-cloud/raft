/*
 * Copyright 2023 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package raftwal

import (
	"context"
	"math"
	"runtime/trace"
	"sync"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"

	"github.com/shaj13/raft/internal/storage"
)

// DiskStorage handles disk access and writing for the RAFT write-ahead log.
// Dir contains wal.meta file and <start idx zero padded>.wal files.
//
// === wal.meta file ===
// This file is generally around 4KB, so it can fit nicely in one Linux page.
//
//	Layout:
//
// 00-08 Bytes: Raft ID
// 08-16 Bytes: Group ID
// 16-24 Bytes: Checkpoint Index
// 512 Bytes: Hard State (Marshalled)
// 1024-1032 Bytes: Snapshot Index
// 1032-1040 Bytes: Snapshot Term
// 1040 Bytes: Snapshot (Marshalled)
//
// --- <0000i>.wal files ---
// These files contain raftpb.Entry protos. Each entry is composed of term, index, type and data.
//
// Term takes 8 bytes. Index takes 8 bytes. Type takes 8 bytes. And for data, we store an offset to
// the actual slice, which is 8 bytes. Size of entry = 32 bytes.
// First 30K entries would consume 960KB, hence fitting on the first MB of the file (logFileOffset).
//
// Pre-allocate 1MB in each file just for these entries, and zero them out explicitly. Zeroing them
// out ensures that we know when these entries end, in case of a restart.
//
// And the data for these entries are laid out starting logFileOffset. Those are the offsets you
// store in the Entry for Data field.
// After 30K entries, we rotate the file.
//
// --- clean up ---
// If snapshot idx = Idx_s. We find the first log file whose first entry is
// less than Idx_s. This file and anything above MUST be kept. All the log
// files lower than this file can be deleted.
//
// --- sync ---
// mmap fares well with process crashes without doing anything. In case
// HardSync is set, msync is called after every write, which flushes those
// writes to disk.
type DiskStorage struct {
	dir string

	meta *metaFile
	wal  *wal
	lock sync.Mutex
}

// Init initializes an instance of DiskStorage without encryption.
func Init(dir string) *DiskStorage {
	ds, err := InitEncrypted(dir, nil)
	Check(err)
	return ds
}

// InitEncrypted initializes returns a properly initialized instance of DiskStorage.
// To gracefully shutdown DiskStorage, store.Closer.SignalAndWait() should be called.
func InitEncrypted(dir string, encKey Sensitive) (*DiskStorage, error) {
	w := &DiskStorage{
		dir: dir,
	}

	var err error
	if w.meta, err = newMetaFile(dir); err != nil {
		return nil, err
	}
	// fmt.Printf("meta: %s\n", hex.Dump(w.meta.data[1024:2048]))
	// fmt.Printf("found snapshot of size: %d\n", sliceSize(w.meta.data, snapshotOffset))

	encryptionKey = encKey
	if w.wal, err = openWal(dir); err != nil {
		return nil, err
	}

	snap, err := w.meta.snapshot()
	if err != nil {
		return nil, err
	}

	first, _ := w.FirstIndex()
	if !raft.IsEmptySnap(snap) {
		AssertTruef(snap.Metadata.Index+1 == first,
			"snap index: %d + 1 should be equal to first: %d\n", snap.Metadata.Index, first)
	}

	// If db is not closed properly, there might be index ranges for which delete entries are not
	// inserted. So insert delete entries for those ranges starting from 0 to (first-1).
	w.wal.deleteBefore(first - 1)
	last := w.wal.LastIndex()

	glog.Infof("Init Raft Storage with snap: %d, first: %d, last: %d\n",
		snap.Metadata.Index, first, last)
	return w, nil
}

func (w *DiskStorage) Boot(m []byte) (meta []byte, hs *raftpb.HardState, ents []raftpb.Entry, snap *storage.Snapshot, err error) {
	w.lock.Lock()
	lo, hi := w.firstIndex(), w.lastIndex()
	w.lock.Unlock()
	meta = m
	// TODO(adphi): handle meta
	if ents, err = w.Entries(lo, hi, math.MaxUint64); err != nil {
		return
	}
	st, err := w.meta.HardState()
	if err != nil {
		return
	}
	hs = &st
	// var sn raftpb.Snapshot
	// if sn, err = w.meta.snapshot(); err != nil {
	// 	return
	// }
	// snap = &storage.Snapshot{SnapshotState: sn}
	return
}

func (w *DiskStorage) SetUint(info MetaInfo, id uint64) { w.meta.SetUint(info, id) }
func (w *DiskStorage) Uint(info MetaInfo) uint64        { return w.meta.Uint(info) }

func (w *DiskStorage) SetMeta(meta []byte) {
	w.meta.SetMeta(meta)
}

func (w *DiskStorage) Meta() []byte {
	return w.meta.Meta()
}

// reset resets the entries. Used for testing.
func (w *DiskStorage) reset(ctx context.Context, es []raftpb.Entry) error {
	// Clean out the state.
	if err := w.wal.reset(); err != nil {
		return err
	}
	return w.addEntries(ctx, es)
}

func (w *DiskStorage) SetHardState(st raftpb.HardState) error {
	return w.meta.StoreHardState(context.Background(), &st)
}

func (w *DiskStorage) HardState() (raftpb.HardState, error) {
	if w.meta == nil {
		return raftpb.HardState{}, errors.Errorf("uninitialized meta file")
	}
	return w.meta.HardState()
}

// Implement the Raft.Storage interface.
// -------------------------------------

// InitialState returns the saved HardState and ConfState information.
func (w *DiskStorage) InitialState() (hs raftpb.HardState, cs raftpb.ConfState, err error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	hs, err = w.meta.HardState()
	if err != nil {
		return
	}
	var snap raftpb.Snapshot
	snap, err = w.meta.snapshot()
	if err != nil {
		return
	}
	return hs, snap.Metadata.ConfState, nil
}

func (w *DiskStorage) NumEntries() int {
	w.lock.Lock()
	defer w.lock.Unlock()

	start := w.wal.firstIndex()

	var count int
	for {
		ents := w.wal.allEntries(start, math.MaxUint64, 64<<20)
		if len(ents) == 0 {
			return count
		}
		count += len(ents)
		start = ents[len(ents)-1].Index + 1
	}
}

// Entries returns a slice of log entries in the range [lo,hi).
// MaxSize limits the total size of the log entries returned, but
// Entries returns at least one entry if any.
func (w *DiskStorage) Entries(lo, hi, maxSize uint64) (es []raftpb.Entry, rerr error) {
	// glog.Infof("Entries: [%d, %d) maxSize:%d", lo, hi, maxSize)
	w.lock.Lock()
	defer w.lock.Unlock()

	// glog.Infof("Entries after lock: [%d, %d) maxSize:%d", lo, hi, maxSize)

	first := w.wal.firstIndex()
	if lo < first {
		glog.Errorf("lo: %d <first: %d\n", lo, first)
		return nil, raft.ErrCompacted
	}

	last := w.wal.LastIndex()
	if hi > last+1 {
		glog.Errorf("hi: %d > last+1: %d\n", hi, last+1)
		return nil, raft.ErrUnavailable
	}

	ents := w.wal.allEntries(lo, hi, maxSize)
	// glog.Infof("got entries [%d, %d): %+v\n", lo, hi, ents)
	return ents, nil
}

func (w *DiskStorage) Term(idx uint64) (uint64, error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	si := w.meta.Uint(SnapshotIndex)
	if idx < si {
		glog.Errorf("TERM for %d = %v\n", idx, raft.ErrCompacted)
		return 0, raft.ErrCompacted
	}
	if idx == si {
		return w.meta.Uint(SnapshotTerm), nil
	}

	term, err := w.wal.Term(idx)
	if err != nil {
		glog.Errorf("TERM for %d = %v\n", idx, err)
	}
	// glog.Errorf("Got term: %d for index: %d\n", term, idx)
	return term, err
}

func (w *DiskStorage) lastIndex() uint64 {
	li := w.wal.LastIndex()
	si := w.meta.Uint(SnapshotIndex)
	if li < si {
		return si
	}
	return li
}

func (w *DiskStorage) LastIndex() (uint64, error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	li := w.wal.LastIndex()
	si := w.meta.Uint(SnapshotIndex)
	if li < si {
		return si, nil
	}
	return li, nil
}

func (w *DiskStorage) firstIndex() uint64 {
	if si := w.Uint(SnapshotIndex); si > 0 {
		return si + 1
	}
	return w.wal.firstIndex()
}

// FirstIndex returns the first index. It is typically SnapshotIndex+1.
func (w *DiskStorage) FirstIndex() (uint64, error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	return w.firstIndex(), nil
}

// Snapshot returns the most recent snapshot.  If snapshot is temporarily
// unavailable, it should return ErrSnapshotTemporarilyUnavailable, so raft
// state machine could know that Storage needs some time to prepare snapshot
// and call Snapshot later.
func (w *DiskStorage) Snapshot() (raftpb.Snapshot, error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	return w.meta.snapshot()
}

// ---------------- Raft.Storage interface complete.

// CreateSnapshot generates a snapshot with the given ConfState and data and writes it to disk.
func (w *DiskStorage) CreateSnapshot(ctx context.Context, i uint64, cs *raftpb.ConfState, data []byte) (raftpb.Snapshot, error) {
	ctx, tr := trace.NewTask(ctx, "raftwal.createSnapshot")
	defer tr.End()

	glog.V(2).Infof("CreateSnapshot i=%d, cs=%+v", i, cs)

	w.lock.Lock()
	defer w.lock.Unlock()

	first := w.firstIndex()
	if i < first {
		glog.Errorf("i=%d<first=%d, ErrSnapOutOfDate", i, first)
		return raftpb.Snapshot{}, raft.ErrSnapOutOfDate
	}

	e, err := w.wal.seekEntry(i)
	if err != nil {
		return raftpb.Snapshot{}, err
	}

	var snap raftpb.Snapshot
	snap.Metadata.Index = i
	snap.Metadata.Term = e.Term()
	AssertTrue(cs != nil)
	snap.Metadata.ConfState = *cs
	snap.Data = data

	if err := w.meta.StoreSnapshot(ctx, &snap); err != nil {
		return raftpb.Snapshot{}, err
	}
	// Now we delete all the files which are below the snapshot index.
	w.wal.deleteBefore(snap.Metadata.Index)
	return snap, nil
}

// SaveEntries would write Entries and HardState to persistent storage in order, i.e. Entries
// first, then HardState if it is not empty. If persistent storage supports atomic
// writes then all of them can be written together. Note that when writing an Entry with Index i,
// any previously-persisted entries with Index >= i must be discarded.
func (w *DiskStorage) SaveEntries(ctx context.Context, h *raftpb.HardState, es []raftpb.Entry) error {
	ctx, tr := trace.NewTask(ctx, "raftwal.saveEntries")
	defer tr.End()
	w.lock.Lock()
	defer w.lock.Unlock()

	old, err := w.meta.HardState()
	if err != nil {
		return err
	}
	if err := w.wal.AddEntries(ctx, es); err != nil {
		return err
	}
	if err := w.meta.StoreHardState(ctx, h); err != nil {
		return err
	}
	if raft.MustSync(old, *h, len(es)) {
		return w.sync(ctx)
	}
	return nil
}

func (w *DiskStorage) SaveSnapshot(ctx context.Context, snap *raftpb.Snapshot) error {
	ctx, tr := trace.NewTask(ctx, "raftwal.saveSnapshot")
	defer tr.End()
	w.lock.Lock()
	defer w.lock.Unlock()

	if err := w.meta.StoreSnapshot(ctx, snap); err != nil {
		return err
	}
	return w.sync(ctx)
}

func (w *DiskStorage) ApplySnapshot(snap raftpb.Snapshot) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.TruncateEntriesUntil(snap.Metadata.Index)
	if err := w.addEntries(context.Background(), []raftpb.Entry{{Index: snap.Metadata.Index, Term: snap.Metadata.Term}}); err != nil {
		return err
	}
	if err := w.meta.StoreSnapshot(context.Background(), &snap); err != nil {
		return err
	}
	return w.sync(context.Background())
}

func (w *DiskStorage) Append(entries []raftpb.Entry) error {
	return w.addEntries(context.Background(), entries)
}

// Append the new entries to storage.
func (w *DiskStorage) addEntries(ctx context.Context, entries []raftpb.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	first, err := w.FirstIndex()
	if err != nil {
		return err
	}
	firste := entries[0].Index
	if firste+uint64(len(entries))-1 < first {
		// All of these entries have already been compacted.
		return nil
	}
	if first > firste {
		// Truncate compacted entries
		entries = entries[first-firste:]
	}

	// AddEntries would zero out all the entries starting entries[0].Index before writing.
	if err := w.wal.AddEntries(ctx, entries); err != nil {
		return errors.Wrapf(err, "while adding entries")
	}
	return nil
}

func (w *DiskStorage) Compact(compactIndex uint64) error {
	w.wal.truncateEntriesUntil(compactIndex)
	return nil
}

// truncateEntriesUntil deletes the data field of every raft entry
// of type EntryNormal and index ∈ [0, lastIdx).
func (w *DiskStorage) TruncateEntriesUntil(lastIdx uint64) {
	w.wal.truncateEntriesUntil(lastIdx)
}

func (w *DiskStorage) NumLogFiles() int {
	return len(w.wal.files)
}

func (w *DiskStorage) sync(ctx context.Context) error {
	ctx, tr := trace.NewTask(ctx, "raftwal.sync")
	defer tr.End()

	if err := w.meta.Sync(); err != nil {
		return errors.Wrapf(err, "while syncing meta")
	}
	if err := w.wal.current.Sync(); err != nil {
		return errors.Wrapf(err, "while syncing current file")
	}
	return nil
}

// Sync calls the Sync method in the underlying badger instance to write all the contents to disk.
func (w *DiskStorage) Sync(ctx context.Context) error {
	w.lock.Lock()
	defer w.lock.Unlock()

	return w.sync(ctx)
}

// Close closes the DiskStorage.
func (w *DiskStorage) Close() error {
	return w.Sync(context.Background())
}

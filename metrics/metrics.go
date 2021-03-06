package metrics

import (
	"sort"
	"sync"
	"time"

	"github.com/getlantern/wal"
)

var (
	leaderStats    *LeaderStats
	followerStats  map[int]*FollowerStats
	partitionStats map[int]*PartitionStats

	mx sync.RWMutex
)

func init() {
	reset()
}

func reset() {
	leaderStats = &LeaderStats{}
	followerStats = make(map[int]*FollowerStats, 0)
	partitionStats = make(map[int]*PartitionStats, 0)
}

// Stats are the overall stats
type Stats struct {
	Leader     *LeaderStats
	Followers  sortedFollowerStats
	Partitions sortedPartitionStats
}

// LeaderStats provides stats for the cluster leader
type LeaderStats struct {
	NumPartitions       int
	ConnectedPartitions int
	ConnectedFollowers  int
	CurrentlyReadingWAL string
}

// FollowerStats provides stats for a single follower
type FollowerStats struct {
	followerId int
	Partition  int
	Queued     int
	Failed     bool
}

// PartitionStats provides stats for a single partition
type PartitionStats struct {
	Partition    int
	NumFollowers int
}

type sortedFollowerStats []*FollowerStats

func (s sortedFollowerStats) Len() int      { return len(s) }
func (s sortedFollowerStats) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s sortedFollowerStats) Less(i, j int) bool {
	if s[i].Partition < s[j].Partition {
		return true
	}
	return s[i].followerId < s[j].followerId
}

type sortedPartitionStats []*PartitionStats

func (s sortedPartitionStats) Len() int      { return len(s) }
func (s sortedPartitionStats) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s sortedPartitionStats) Less(i, j int) bool {
	return s[i].Partition < s[j].Partition
}

// SetNumPartitions sets the number of partitions in the cluster
func SetNumPartitions(numPartitions int) {
	mx.Lock()
	leaderStats.NumPartitions = numPartitions
	mx.Unlock()
}

// CurrentlyReadingWAL indicates that we're currently reading the WAL at a given offset
func CurrentlyReadingWAL(offset wal.Offset) {
	ts := offset.TS()
	mx.Lock()
	leaderStats.CurrentlyReadingWAL = ts.Format(time.RFC3339)
	mx.Unlock()
}

// FollowerJoined records the fact that a follower joined the leader
func FollowerJoined(followerID int, partition int) {
	mx.Lock()
	defer mx.Unlock()
	fs := getFollowerStats(followerID)
	fs.Partition = partition
	ps := partitionStats[partition]
	if ps == nil {
		ps = &PartitionStats{Partition: partition}
		partitionStats[partition] = ps
		leaderStats.ConnectedPartitions++
	}
	ps.NumFollowers++
}

// FollowerFailed records the fact that a follower failed (which is analogous to leaving)
func FollowerFailed(followerID int) {
	mx.Lock()
	defer mx.Unlock()
	// Only mark failed once
	fs, found := followerStats[followerID]
	if found && !fs.Failed {
		leaderStats.ConnectedFollowers--
		fs.Failed = true
		partitionStats[fs.Partition].NumFollowers--
		if partitionStats[fs.Partition].NumFollowers == 0 {
			leaderStats.ConnectedPartitions--
		}
	}
}

// QueuedForFollower records how many measurements are queued for a given Follower
func QueuedForFollower(followerID int, queued int) {
	mx.Lock()
	defer mx.Unlock()
	fs, found := followerStats[followerID]
	if found {
		fs.Queued = queued
	}
}

func getFollowerStats(followerID int) *FollowerStats {
	fs, found := followerStats[followerID]
	if !found {
		leaderStats.ConnectedFollowers++
		fs = &FollowerStats{
			followerId: followerID,
			Queued:     0,
		}
		followerStats[followerID] = fs
	}
	return fs
}

func GetStats() *Stats {
	mx.RLock()
	s := &Stats{
		Leader:     leaderStats,
		Followers:  make(sortedFollowerStats, 0, len(followerStats)),
		Partitions: make(sortedPartitionStats, 0, len(partitionStats)),
	}

	for _, fs := range followerStats {
		s.Followers = append(s.Followers, fs)
	}
	for _, ps := range partitionStats {
		s.Partitions = append(s.Partitions, ps)
	}
	mx.RUnlock()

	sort.Sort(s.Followers)
	sort.Sort(s.Partitions)
	return s
}

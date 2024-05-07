package raft

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"math/rand"
	"os"
	"p5/pb"
	"strconv"
	"sync"
	"time"
)

const (
	// Adjust these values based on your Raft configuration
	minElectionTimeout = 2000 * time.Millisecond
	maxElectionTimeout = 3000 * time.Millisecond
	heartbeatInterval  = 1000 * time.Millisecond
	// minElectionTimeout = 6000 * time.Millisecond
	// maxElectionTimeout = 12000 * time.Millisecond
	// heartbeatInterval  = 1000 * time.Millisecond
)

const (
	Follower  = 0
	Candidate = 1
	Leader    = 2
)

var (
	leaderId                int32
	votesReceived           int // Number of votes received in the current term
	leaderTerm              int32
	Count                   int
	mu                      sync.Mutex // Mutex to protect shared access to state
	StopQueuedLoguesRoutine chan bool
	IsQueuedRoutineRunning  bool
	LeaderAnnouncement      string
	ServerLog               *Log
	Queue                   map[string]*LogQueue
	currentTerm             int32
	votedFor                int32
	SuccessCount            map[int32]int
	nextIndex               int32
	LogFile                 *os.File
	combined                string
	role                    int
	ElectionTicker          *time.Ticker
	LeaderAddress           string
	SavedTime               time.Time
)

type LogQueue struct {
	entries []LogEntry
}

type LogEntry struct {
	Term      int32
	Index     int32
	Command   string
	PrevTerm  int32
	Committed bool
}

type Log struct {
	entries     []LogEntry
	commitIndex int32
	lastApplied int32
}

func NewRaftClient(conn *grpc.ClientConn) pb.RaftClient {
	return pb.NewRaftClient(conn)
}

type RaftServer struct {
	pb.UnimplementedRaftServer
}

func InitElectionTimer() {
	role = Follower
	go func() {
		for {
			SavedTime = time.Now()
			select {
			case <-ElectionTicker.C:
				if time.Since(SavedTime) > 3*time.Second {
					role = Follower
					//StopQueuedLogsRoutine()
					time.Sleep(2 * time.Second)
					continue
				}
				StartElection()
			}
		}
	}()
}

func RandomElectionTimeout() time.Duration {
	randDuration := minElectionTimeout + time.Duration(rand.Intn(int(maxElectionTimeout-minElectionTimeout)))
	return randDuration
}

func StartElection() {
	mu.Lock()
	defer mu.Unlock()
	currentTerm++
	fmt.Println("startElection currentTerm:", currentTerm)
	//votedFor = int32(MeID)
	votesReceived = 1
	role = Candidate
	//StopQueuedLogsRoutine()
	ElectionTicker.Reset(RandomElectionTimeout())
	for _, server := range Servers {
		//fmt.Println("StartElection------", server, MeServer)
		if server != MeServer {
			go SendRequestVoteRPCs(server)
		}
	}
}

func (s *RaftServer) RequestVote(ctx context.Context, request *pb.VoteRequest) (*pb.VoteReply, error) {
	mu.Lock()
	defer mu.Unlock()

	reply := &pb.VoteReply{
		Term:        currentTerm,
		VoteGranted: false,
	}

	if request.Term < currentTerm {
		return reply, nil
	}

	if request.Term >= currentTerm && isCandidateLogUpToDate(request.LastLogIndex, request.LastLogTerm) {
		currentTerm = request.Term
		//votedFor = -1 // Reset vote
		role = Follower
		//StopQueuedLogsRoutine()
		votedFor = request.CandidateId
		reply.VoteGranted = true
		//fmt.Println("-----------1111111111---------")
		//ResetElectionTicker()
	}
	/*
		if votedFor == -1 && isCandidateLogUpToDate(request.LastLogIndex, request.LastLogTerm) {
			votedFor = request.CandidateId
			reply.VoteGranted = true
		}

	*/

	return reply, nil
}

func (s *RaftServer) Catchup(ctx context.Context, request *pb.CatchupRequest) (*pb.CatchupReply, error) {
	fmt.Println("catchup", request.ServerAddress, "trying PrevLogIndex", request.Index)
	reply := &pb.CatchupReply{
		Command: ServerLog.entries[request.Index].Command,
		Term:    ServerLog.entries[request.Index].Term,
	}
	return reply, nil
}

func (s *RaftServer) AppendEntries(ctx context.Context, request *pb.AppendEntriesRequest) (*pb.AppendEntriesReply, error) {
	//fmt.Println("######", len(ServerLog.entries), ServerLog.lastApplied, ServerLog.commitIndex)
	ElectionTicker.Reset(RandomElectionTimeout())
	//fmt.Println("AppendEntries-----------0-------------")
	SavedTime = time.Now()
	LeaderAddress = request.LeaderAddress

	if ServerLog.commitIndex > ServerLog.lastApplied && role != Leader {
		//fmt.Println(len(ServerLog.entries), ServerLog.lastApplied, ServerLog.commitIndex)
		//fmt.Println("AppendEntries-----------1-------------")
		for _, entry := range ServerLog.entries[ServerLog.lastApplied:min(int32(len(ServerLog.entries)), ServerLog.commitIndex+1)] {
			//fmt.Println("AppendEntries-----------2-------------")
			//fmt.Println("------------here---", len(ServerLog.entries), ServerLog.lastApplied, ServerLog.commitIndex)
			entry.Committed = true
			ServerLog.lastApplied++
			command := strconv.Itoa(int(entry.Term)) + "-" + entry.Command + "\n"
			_, err := LogFile.WriteString(command)
			if err != nil {
				fmt.Println(err)
				//return
			}
		}
	}

	nextIndex = request.PrevLogIndex + 1
	/*

		if ServerLog.LastIndex() < ServerLog.commitIndex && currentTerm <= request.Term {
			//if ServerLog.LastIndex() < nextIndex-2 && currentTerm <= request.Term {
			//fmt.Println("leaderId:", leaderId)
			//var serverAddress string
			for _, server := range Servers {
				if strings.HasSuffix(server, strconv.Itoa(int(leaderId))) {
					//serverAddress = server
					for {
						if ServerLog.LastIndex() < nextIndex-1 {
							CatchupWithLeader(ServerLog.LastIndex(), server)
						} else {
							break
						}
					}
					break
				}
			}

		}

	*/
	//fmt.Println("AppendEntries-----------3-------------")
	if int(nextIndex) >= 0 && int(nextIndex) < len(ServerLog.entries) &&
		ServerLog.entries[nextIndex].Term != request.Term {
		//fmt.Println("AppendEntries-----------4-------------")
		ServerLog.entries = ServerLog.entries[:nextIndex]
		//fmt.Println("+++++++", nextIndex)
	}

	if request.LeaderCommit > ServerLog.commitIndex && request.LeaderId != int32(MeID) {
		ServerLog.commitIndex = min(request.LeaderCommit, nextIndex)
		//StopQueuedLogsRoutine()
		//fmt.Println("AppendEntries-----------5-------------")
	}

	reply := &pb.AppendEntriesReply{
		Term:    currentTerm,
		Success: false,
	}

	if int32(MeID) != request.LeaderId && request.Term >= currentTerm {
		//fmt.Println("AppendEntries-----------6-------------")
		leaderId = request.LeaderId

		role = Follower
		//StopQueuedLogsRoutine()
		currentTerm = request.Term
		//ElectionTicker.Reset(RandomElectionTimeout())
	}

	if request.Heartbeat {
		//fmt.Println("--------3---------")
		//fmt.Println("AppendEntries-----------7-------------")
		if role == Leader {
			fmt.Print("AE ci:", ServerLog.lastApplied-1, ", ", Count, "[", combined, "]\n")
		} else {
			fmt.Print("ci:", ServerLog.lastApplied-1, ", ", Count, "[", combined, "]\n")
		}

		//fmt.Println("--------4---------")
		return reply, nil
	}
	//fmt.Println("--------1---------")
	//fmt.Println("-----prev", ServerLog.LastTerm(), request.PrevLogTerm)
	//fmt.Println("--------2---------")

	if request.Term >= currentTerm && ServerLog.LastTerm() == request.PrevLogTerm {
		//fmt.Println("AppendEntries-----------8-------------")
		mu.Lock()
		index := ServerLog.AppendEntry(request.Command, int(request.Term))
		mu.Unlock()
		//ServerLog.commitIndex = index
		//logEntry := ServerLog.GetEntryAt(request.PrevLogIndex)
		logEntry := ServerLog.GetEntryAt(index)
		reply.Success = true

		if len(ServerLog.entries) > 1 {
			combined += ","
		}

		combined += strconv.Itoa(int(logEntry.Term)) + "-\"" + logEntry.Command + "\""

		//combined += strconv.Itoa(int(logEntry.Term)) + "-\"" + logEntry.Command + "\","

		Count++

	}
	return reply, nil
}

func (s *RaftServer) LeaderServerAddress(ctx context.Context, request *pb.LeaderServerRequest) (*pb.LeaderServerReply, error) {
	reply := &pb.LeaderServerReply{LeaderID: leaderId}
	return reply, nil
}

func (s *RaftServer) Command(ctx context.Context, request *pb.CommandRequest) (*pb.CommandReply, error) {

	index := ServerLog.AppendEntry(request.Command, int(currentTerm))

	logEntry := ServerLog.GetEntryAt(index)

	if len(ServerLog.entries) > 1 {
		combined += ","
	}

	combined += strconv.Itoa(int(logEntry.Term)) + "-\"" + logEntry.Command + "\""
	Count++
	success := sendLogsToServers(request.Command, currentTerm, ServerLog.LastTerm(), index)

	reply := &pb.CommandReply{
		LogIndex: int64(index) - 1,
		Success:  success,
	}

	//logEntry := ServerLog.GetEntriesAfter(0)

	return reply, nil
}

// AppendEntry appends a new entry to the log.
func (l *Log) AppendEntry(command string, term int) int32 {

	entry := LogEntry{
		Command: command,
		Term:    int32(term),
		Index:   l.LastIndex() + 1,
	}
	//fmt.Println("AppendEntry", entry)
	l.entries = append(l.entries, entry)
	return l.LastIndex()
}

// LastIndex returns the index of the last entry in the log.
func (l *Log) LastIndex() int32 {
	if len(l.entries) == 0 {
		return 0
	}
	return l.entries[len(l.entries)-1].Index
}

func (l *Log) LastTerm() int32 {
	if len(l.entries) == 0 {
		return 0
	}
	//fmt.Println("&&&&&&&&index:", len(l.entries)-1)
	return l.entries[len(l.entries)-1].Term
}

func (l *Log) LastTerm2(PrevLogIndex int32) int32 {
	if len(l.entries) == 0 {
		return 0
	}
	logEntry := ServerLog.GetEntryAt(PrevLogIndex)
	return logEntry.Term
}

// SetCommitIndex sets the commit index. This should only be set to a value
// that represents a log entry known to be committed.
func (l *Log) SetCommitIndex(index int32) {
	if index > l.LastIndex() {
		return // Index out of bounds, do not update
	}
	l.commitIndex = index
}

// GetEntriesAfter returns all the entries after a given index.
func (l *Log) GetEntriesAfter(index int32) []LogEntry {
	for i, entry := range l.entries {
		if entry.Index == index+1 {
			return l.entries[i:]
		}
	}
	return nil
}

// GetEntryAt returns the log entry at the given index, if it exists.
func (l *Log) GetEntryAt(index int32) LogEntry {
	if index <= 0 || index > l.LastIndex() {
		return LogEntry{}
	}
	// Since Index is 1-based and slice is 0-based, subtract 1
	return l.entries[index-1]
}

func isCandidateLogUpToDate(candidateLastLogIndex, candidateLastLogTerm int32) bool {
	//mu.Lock()
	//defer mu.Unlock()
	var lastLogTerm int32

	lastLogIndex := ServerLog.LastIndex()

	if lastLogIndex > 0 && len(ServerLog.entries) > 0 {
		lastLogTerm = ServerLog.entries[lastLogIndex-1].Term
	} else {
		lastLogTerm = 0
	}

	if candidateLastLogTerm != lastLogTerm {
		return candidateLastLogTerm > lastLogTerm
	}

	return candidateLastLogIndex >= lastLogIndex
}

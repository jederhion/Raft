package raft

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
	"p5/pb"
	"strconv"
	"strings"
	"time"
)

var (
	Servers       []string
	MeServer      string
	MeID          int64
	LastLogIndex  int64
	MatchIndex    []int64
	ServerClients = make(map[string]pb.RaftClient)
	Host          string
)

func SendRequestVoteRPCs(server string) {
	var lastLogTerm int32
	mu.Lock()
	lastLogIndex := ServerLog.LastIndex()
	if lastLogIndex > 0 && len(ServerLog.entries) > 0 {
		lastLogTerm = ServerLog.entries[lastLogIndex-1].Term
	} else {
		// Handle the case where the log is empty. You might want to default to term 0 or some other logic.
		lastLogTerm = 0
	}

	//fmt.Println("SendRequest() lastIndex:", lastLogIndex, "lastTerm:", lastLogTerm)

	request := &pb.VoteRequest{
		Term:         currentTerm,
		CandidateId:  int32(MeID),
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	mu.Unlock()

	conn, err := grpc.Dial(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("SendRequestVoteRPCs(): Failed to connect to server at %s: %v", server, err)
		return
	}
	defer conn.Close()

	client := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reply, err := client.RequestVote(ctx, request)
	if err != nil {
		//log.Printf("SendRequestVoteRPCs(): Failed to send RequestVote RPC to %s: %v", server, err)
		return
	}

	// Check if reply is from an outdated term
	//fmt.Println("reply:", reply.Term, "current:", currentTerm)
	if reply.Term > currentTerm {
		//fmt.Println("SendRequestVoteRPCs-----------0-----------")
		role = Follower
		//StopQueuedLogsRoutine()
		ElectionTicker.Reset(RandomElectionTimeout())
		return
	}

	// Only count the votes if we are still a candidate and the term matches
	if role == Candidate && currentTerm == request.Term {
		if reply.VoteGranted {
			votesReceived++
			if votesReceived > len(Servers)/2 {

				ElectionTicker.Reset(RandomElectionTimeout())
				role = Leader
				leaderId = int32(MeID)
				index := ServerLog.LastIndex() + 1
				//index := ServerLog.LastIndex() - 1
				prevTerm := ServerLog.LastTerm()
				//fmt.Println("SendRequestVoteRPCs---------------0-------------")
				for _, serverAddress := range Servers {
					go leaderAnnouncement(index, prevTerm, serverAddress)
				}

				//fmt.Println("SendRequestVoteRPCs---------------1-------------")
				leaderTerm = currentTerm
				fmt.Println("leader term", currentTerm)
				//ElectionTicker.Stop()
				cmd := strconv.Itoa(int(currentTerm)) + "-" + LeaderAnnouncement + "\n"
				ServerLog.lastApplied++
				ServerLog.commitIndex = index
				//fmt.Println("SendRequestVoteRPCs---------------2-------------")
				_, _ = LogFile.WriteString(cmd)
				StartQueuedLogsRoutine()
				//fmt.Println("SendRequestVoteRPCs---------------3-------------")
				go startHeartbeatProcess()
				//fmt.Println("SendRequestVoteRPCs---------------4-------------")

			}
		}
	}
}

func startHeartbeatProcess() {
	//fmt.Println("startHeartbeatProcess------------0--------------------")
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		SavedTime = time.Now()
		select {
		case <-ticker.C:
			if time.Since(SavedTime) > 3*time.Second {
				role = Follower
				//StopQueuedLogsRoutine()
			}
			//fmt.Println("startHeartbeatProcess------------1--------------------")
			//fmt.Println("StartHeartBeatProcess  Role:", role)
			if role == Leader {
				//fmt.Println("startHeartbeatProcess------------2--------------------")
				broadcastHeartbeat()
			} else {
				return
			}
		}
	}
}

func broadcastHeartbeat() {
	ElectionTicker.Reset(RandomElectionTimeout())
	//fmt.Println("broadcastHeartbeat--------0----------------")
	for _, server := range Servers {
		//fmt.Println("broadcastHeartbeat--------1----------------", server)
		go sendHeartbeat(server)
		if role == Follower {
			//fmt.Println("broadcastHeartbeat--------2----------------")
			return
		}
	}
}

func leaderAnnouncement(index, prevTerm int32, serverAddress string) bool {

	conn, err := grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to follower at %s: %v", serverAddress, err)
		return false
	}
	defer conn.Close()

	client := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//fmt.Println("Leaderannouncement to:", serverAddress, "Term:", currentTerm)
	ElectionTicker.Reset(RandomElectionTimeout())

	LeaderAnnouncement = "L-" + strconv.Itoa(int(currentTerm)) + "-" + strconv.Itoa(int(MeID))

	//fmt.Println("**********prevLogTerm:", prevTerm)
	request := &pb.AppendEntriesRequest{
		LeaderId:     int32(MeID),
		Term:         currentTerm,
		Command:      LeaderAnnouncement,
		PrevLogIndex: index,
		PrevLogTerm:  prevTerm,
	}
	_, err = client.AppendEntries(ctx, request)
	if err != nil {
		//fmt.Println("leaderAnnouncement()----", serverAddress)
		if Queue[serverAddress] == nil {
			Queue[serverAddress] = &LogQueue{}
			Queue[serverAddress].entries = []LogEntry{{Command: LeaderAnnouncement,
				Term: currentTerm, PrevTerm: prevTerm, Index: index}}
			//fmt.Println("leaderAnnouncement() queue if block")
		} else {
			//fmt.Println("+++++++++", command, term)
			//fmt.Println("leaderAnnouncement() queue else block")
			Queue[serverAddress].entries = append(Queue[serverAddress].entries,
				LogEntry{Command: LeaderAnnouncement, Term: currentTerm, PrevTerm: prevTerm, Index: index})
		}
		//log.Printf("sendHeartbeat(): Failed to send heartbeat to follower at %s: %v", serverAddress, err)

	}
	return true

}

func sendHeartbeat(serverAddress string) {
	//fmt.Println("sendHeartbeat--------0----------------")
	if role == Follower {
		return
	}
	conn, err := grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to follower at %s: %v", serverAddress, err)
		return
	}
	defer conn.Close()

	client := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//fmt.Println("sendheartbeat to:", serverAddress, "Term:", currentTerm)

	request := &pb.AppendEntriesRequest{
		Heartbeat:    true,
		Term:         currentTerm,
		LeaderId:     int32(MeID),
		LeaderCommit: ServerLog.commitIndex,
		PrevLogIndex: ServerLog.LastIndex(),
	}
	response, err := client.AppendEntries(ctx, request)
	if err != nil {

		//log.Printf("sendHeartbeat(): Failed to send heartbeat to follower at %s: %v", serverAddress, err)
		return
	} /*else {
		StopQueuedLogsRoutine()
	}
	*/
	if response.Term > leaderTerm {
		//fmt.Println("sendHeartbeat()---------10-------------")
		currentTerm = response.Term
		leaderId = response.Term
		role = Follower
		//StopQueuedLogsRoutine()
	}
}

func sendLogs(command string, term, prevTerm, index int32, serverAddress string) (bool, int32) {

	conn, err := grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		//fmt.Println("SendLogs()-----------0---------------", serverAddress, err)
		log.Printf("Failed to connect to follower at %s: %v", serverAddress, err)
		return false, 0
	}
	defer conn.Close()

	client := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//fmt.Println("************* sendLogs()", role, leaderId, currentTerm)

	request := &pb.AppendEntriesRequest{
		Heartbeat:    false,
		Term:         term,
		LeaderId:     int32(MeID),
		PrevLogIndex: index,
		PrevLogTerm:  prevTerm,
		Command:      command,
	}

	response, err := client.AppendEntries(ctx, request)
	if err != nil {
		//fmt.Println("SendLogs()-----------1---------------", serverAddress, err)
		//log.Printf("sendHeartbeat(): Failed to send heartbeat to follower at %s: %v", serverAddress, err)
		return false, 0
	}

	return response.Success, response.Term
}

/*
func StartQueuedLogsRoutine() {
	//var mutex sync.RWMutex
	if role != Leader {
		return
	}
	StopQueuedLoguesRoutine = make(chan bool) // initialize the quit channel

	for _, server := range Servers {
		go func(s string) {
			for {
				select {
				case <-StopQueuedLoguesRoutine: // if a signal is sent to the quit channel, stop the goroutine
					return
				default:
					if s == MeServer {
						return
					}
					IsQueuedRoutineRunning = true
					mu.RLock()
					if Queue[s] != nil && len(Queue[s].entries) > 0 {
						head := Queue[s].entries[0]
						//fmt.Println("***********StartQueuedLogsRoutine()", s)
						success, _ := sendLogs(head.Command, head.Term, head.PrevTerm, head.Index, s)
						if success {
							//fmt.Println("catchup", s, "trying PrevLogIndex", head.Index)
							fmt.Println("catchup", s, "trying PrevLogIndex", head.Index)

							SuccessCount[head.Index]++
							if Queue[s] != nil && len(Queue[s].entries) > 0 {
								Queue[s].entries = Queue[s].entries[1:]
							}
						}
					}
					mu.RUnlock()
				}
			}
		}(server)
	}
}

*/

func StartQueuedLogsRoutine() {
	//var mutex sync.RWMutex

	StopQueuedLoguesRoutine = make(chan bool) // initialize the quit channel

	for _, server := range Servers {
		go func(s string) {
			for {

				select {
				case <-StopQueuedLoguesRoutine: // if a signal is sent to the quit channel, stop the goroutine
					return
				default:
					if s == MeServer || role != Leader {
						return
					}
					IsQueuedRoutineRunning = true
					mu.Lock()
					if Queue[s] != nil && len(Queue[s].entries) > 0 {
						head := Queue[s].entries[0]
						//fmt.Println("***********StartQueuedLogsRoutine()", s)

						success, _ := sendLogs(head.Command, head.Term, head.PrevTerm, head.Index, s)
						//fmt.Println("status", success, "Command:", head.Command, head.Term, head.PrevTerm, head.Index, s)
						if success {
							//fmt.Println("catchup", s, "trying PrevLogIndex", head.Index)
							fmt.Println("catchup", s, "trying PrevLogIndex", head.Index)

							SuccessCount[head.Index]++
							if Queue[s] != nil && len(Queue[s].entries) > 0 {
								Queue[s].entries = Queue[s].entries[1:]
							}
						}
					}
					mu.Unlock()
				}
			}
		}(server)
	}
}

func StopQueuedLogsRoutine() {
	if IsQueuedRoutineRunning { // check if any goroutines are running
		StopQueuedLoguesRoutine <- true // send a signal to the quit channel to stop the goroutines
	}
}

func sendLogsToServers(command string, term, prevTerm, index int32) bool {

	SuccessCount[index] = 1
	success := false

	for _, server := range Servers {
		if server != MeServer {
			success, _ = sendLogs(command, term, prevTerm, index, server)
			if success {
				SuccessCount[index]++
			} else {
				mu.Lock()
				if Queue[server] == nil {
					Queue[server] = &LogQueue{}
					Queue[server].entries = []LogEntry{{Command: command, Term: term, PrevTerm: prevTerm,
						Index: index}}
				} else {

					Queue[server].entries = append(Queue[server].entries, LogEntry{Command: command,
						Term: term, PrevTerm: prevTerm, Index: index})
				}
				mu.Unlock()
			}
		}
	}

	for {
		if SuccessCount[index] > len(Servers)/2 {
			ServerLog.entries[index-1].Committed = true
			ServerLog.commitIndex = index
			//CommitIndex++

			cmd := strconv.Itoa(int(term)) + "-" + command + "\n"
			_, err := LogFile.WriteString(cmd)
			ServerLog.lastApplied++
			if err != nil {
				fmt.Println(err)
				return false
			}
			success = true
			break
		}
	}
	return success
}

func ReadLogFile(logfile *os.File) {

	scanner := bufio.NewScanner(logfile)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "-", 2) // split the line on the first hyphen

		if len(parts) != 2 {
			fmt.Println("Invalid line:", line)
			continue
		}
		num, _ := strconv.ParseInt(parts[0], 10, 32)
		//fmt.Println(parts[1], int(num))
		index := ServerLog.AppendEntry(parts[1], int(num))
		ServerLog.commitIndex = index
		ServerLog.lastApplied++
		currentTerm = int32(num)

		if len(ServerLog.entries) > 1 {
			combined += ","
		}
		//combined += strconv.Itoa(int(num)) + "-\"" + parts[1] + "\","

		combined += strconv.Itoa(int(num)) + "-\"" + parts[1] + "\""

		Count++
	}
}

/*
func CatchupWithLeader(currentIndex int32, serverAddress string) {
	conn, err := grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("CatchupWithLeader() Failed to connect to Leader at %s: %v", serverAddress, err)
		return
	}
	defer conn.Close()

	client := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	request := &pb.CatchupRequest{
		Index:         currentIndex,
		ServerAddress: MeServer,
	}

	response, err := client.Catchup(ctx, request)
	if err != nil {
		//log.Printf("sendHeartbeat(): Failed to send heartbeat to follower at %s: %v", serverAddress, err)
		return
	}

	//fmt.Println("Before Length:", len(ServerLog.entries), "index:", currentIndex,
	//	"command:", response.Command, response.Term)
	_ = ServerLog.AppendEntry(response.Command, int(response.Term))
	currentTerm = response.Term

	//fmt.Println("After Length:", len(ServerLog.entries))

	if len(ServerLog.entries) > 1 {
		combined += ","
	}

	combined += strconv.Itoa(int(response.Term)) + "-\"" + response.Command + "\""

	Count++
}

*/

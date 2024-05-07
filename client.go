package main

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"os"
	"p5/pb"
	"strconv"
	"strings"
	"time"
	// Add other necessary imports
)

var (
	Debug bool
)

func main() {
	passert(len(os.Args) > 2, "USAGE: cli <serv> <cmd>\n")
	server, cmd := os.Args[1], os.Args[2]

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Printf("Oops: %v\n", err)
		return
	}
	port := strings.Split(server, ":")[1]

	serverAddress := hostname + ":" + port

	pdebug("Dialing %s\n", server)

	leaderId := LeaderAddress(serverAddress)

	serverAddress = serverAddress[:len(serverAddress)-1] + leaderId

	//fmt.Println("++++serveraddress", leaderAddress)

	//fmt.Println("++++LeaderAddress", serverAddress)

	conn, err := grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	pdebug("DIALED %s\n", serverAddress)
	passert(err == nil, "main() could not connect to %q: %v\n", serverAddress, err)

	cli := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 555*time.Second)
	defer cancel()

	reply, err := cli.Command(ctx, &pb.CommandRequest{Command: cmd})
	passert(err == nil, "Assert error on sending client request %q to %q: %v\n", cmd, server, err)

	palways("cmd %q: commit %t, log index %d\n", cmd, reply.Success, reply.LogIndex)
}

//=====================================================================

func LeaderAddress(server string) string {
	conn, err := grpc.Dial(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
	pdebug("DIALED %s\n", server)
	//fmt.Println("--------------", server)
	passert(err == nil, "LeaderAddress() could not connect to %q: %v\n", server, err)

	cli := pb.NewRaftClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 555*time.Second)
	defer cancel()

	reply, err := cli.LeaderServerAddress(ctx, &pb.LeaderServerRequest{})
	if err != nil {
		fmt.Println("LeaderAddress()", err)
	}
	//fmt.Println("reply:", reply.LeaderID)
	return strconv.Itoa(int(reply.LeaderID))
}

func pdebug(s string, args ...interface{}) {
	if !Debug {
		return
	}
	fmt.Printf(s, args...)
}

func palways(s string, args ...interface{}) {
	fmt.Printf(s, args...)
}

func pexit(s string, args ...interface{}) {
	fmt.Printf(s, args...)
	os.Exit(0)
}

func passert(cond bool, s string, args ...interface{}) {
	if !cond {
		pexit(s, args...)
	}
}

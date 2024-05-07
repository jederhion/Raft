package main

import (
	"errors"
	"flag"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
	"math/rand"
	"net"
	"os"
	"p5/pb"
	. "p5/rpc_files"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =====================================================================

//var serverClients []string

//var serverClients map[string]*grpc.ClientConn

func StartConnections() {
	var w sync.WaitGroup
	var mutex sync.Mutex
	for _, server := range Servers {
		if server != MeServer {
			w.Add(1)
			go func(server string) {
				// Set up a connection to the server
				//pdebug("Dialing %s\n", server)
				conn, err := grpc.Dial(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
				//pdebug("DIALED %s\n", server)
				passert(err == nil, "could not connect to %q: %v\n", server, err)
				mutex.Lock()
				ServerClients[server] = NewRaftClient(conn)
				mutex.Unlock()
				//pdebug("CLIENTMAP %q: %v\n", server, serverClients)
				//pdebug("Set up to %q\n", server)
				w.Done()

			}(server)
		}
	}
	w.Wait()
}

//=====================================================================

func main() {
	LastLogIndex = -1

	//Pause = make(chan bool)

	Host, _ = os.Hostname()

	//flag.BoolVar(&Debug, "d", false, "toggle debug")
	sstr := ""
	flag.StringVar(&sstr, "s", sstr, "<ME srv:port>,<srv 0:port>,<srv 1:port>...,")

	flag.Parse()

	//=====================================================================
	// must be other servers, not including self
	if sstr != "" {
		Servers = strings.Split(sstr, ",")
		MeServer, Servers = Servers[0], Servers[1:]
		foundMe := false
		for i, s := range Servers {
			if s == MeServer {
				MeID = int64(i)
				foundMe = true
				break
			}
		}
		temp := strings.Split(MeServer, ":")
		lastElement := temp[len(temp)-1]
		//lastElement := temp1[len(temp1)-1]
		lastCharacter := lastElement[len(lastElement)-1]
		lastCharacterInt, err := strconv.Atoi(string(lastCharacter))
		if err != nil {
			fmt.Println("Error converting last character to integer:", err)
		}
		MeID = int64(lastCharacterInt)
		//fmt.Println("-------------", lastCharacterInt)
		passert(foundMe, "could not find %q in %q\n", MeServer, Servers)
	} else {
		MeServer, Servers = getDockerComposeServers()
	}
	MatchIndex = make([]int64, len(Servers))
	for i := range Servers {
		MatchIndex[i] = -1
	}

	StartConnections()

	serverPort := MeServer[strings.Index(MeServer, ":"):]

	//=====================================================================

	lis, err := net.Listen("tcp", serverPort)
	passert(err == nil, "failed to listen: %v", err)

	s := grpc.NewServer()

	//raftServer := NewRaftServer()

	fileName := "Log" + strings.Split(MeServer, ":")[1]

	//fmt.Println("============FileName:", fileName)

	pb.RegisterRaftServer(s, &RaftServer{})
	//ServerLog = NewLog()
	ServerLog = &Log{}
	LogFile, err = os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer LogFile.Close()

	//ReadLogFile(LogFile)

	//fmt.Println("--------", MeServer)

	Queue = make(map[string]*LogQueue)
	SuccessCount = make(map[int32]int)
	ElectionTicker = time.NewTicker(RandomElectionTimeout())
	defer ElectionTicker.Stop()
	//fmt.Println("+++++++++++++++++++here++++++++++++++++++++++")
	InitElectionTimer()
	//fmt.Println("------------------------here------------")
	//go ApplyLogsToStateMachine()

	palways("And we are up: meID %v, : meServer %q, all servers: %v\n", MeID, MeServer, Servers)

	if err := s.Serve(lis); err != nil {
		pexit("failed to serve: %v", err)
	}
}

//=====================================================================

func randomMsecDuration(from, to time.Duration) time.Duration {
	dur := time.Duration(time.Duration(rand.Intn(int(to-from))) + from)
	// palways("random %v    (from %v   and   %v)\n", dur, from, to)
	return dur
}

//=====================================================================

func pdebug(s string, args ...interface{}) {
	/*if !Debug {
		return
	}

	*/
	fmt.Printf(s, args...)
}

func palways(s string, args ...interface{}) {
	fmt.Printf(s, args...)
}

func pexit(s string, args ...interface{}) {
	fmt.Printf(s, args...)
	os.Exit(1)
}

func passert(cond bool, s string, args ...interface{}) {
	if !cond {
		pexit(s, args...)
	}
}

func pif(cond bool, s string, args ...interface{}) {
	if cond {
		palways(s, args...)
	}
}

func MakeError(s string, args ...interface{}) error {
	return errors.New(fmt.Sprintf(s, args...))
}

//=====================================================================

type DockerCompose struct {
	Version  string             `yaml:"version"`
	Services map[string]Service `yaml:"services"`
}

type Service struct {
	ContainerName string   `yaml:"container_name"`
	Hostname      string   `yaml:"hostname"`
	Ports         []string `yaml:"ports"`
}

func getDockerComposeServers() (string, []string) {
	var dockerCompose DockerCompose

	yamlFile, err := os.ReadFile("compose.yaml")
	passert(err == nil, "Error reading Docker Compose file: %v", err)

	// Unmarshal the YAML data
	err = yaml.Unmarshal(yamlFile, &dockerCompose)
	passert(err == nil, "Error unmarshaling Docker Compose YAML: %v", err)

	// Iterate through the services and print hostname:port information
	for _, service := range dockerCompose.Services {
		if len(service.Ports) > 0 {
			port := service.Ports[0]
			Servers = append(Servers, service.Hostname+port[strings.Index(port, ":"):])
		}
	}
	sort.Strings(Servers)

	hname, _ := os.Hostname()

	for i, s := range Servers {
		if strings.Index(s, hname) >= 0 {
			MeServer = s
			MeID = int64(i)
			break
		}
	}
	//pdebug("host: %q, meID: %d, MeServer: %s, Servers: %v\n", hname, meID, MeServer, Servers)
	passert((MeServer != "") && (len(Servers) >= 2), "Error: badly formatted MeServer %q, or Servers %v\n", MeServer, Servers)
	return MeServer, Servers
}

package main

import (
    "bufio"
    "flag"
    "fmt"
    "os"
    "net"
    "regexp"
    "strconv"
    "strings"
    "sync"
    "time"
)

const usage  = "usage: lineserver -p port [-t max_threads] filename"

var listen_port int
var max_clients int
var total_clients uint64

//
//  ServerState object and methods - convenience object for managing the server
//
type ServerState struct {
    rw_lock sync.RWMutex
    shutdown bool
    wg sync.WaitGroup
}

func (s *ServerState) IsShutdown() bool {
    s.rw_lock.RLock()
    state := s.shutdown
    s.rw_lock.RUnlock()
    return state
}

func (s *ServerState) InitiateShutdown() {
    s.rw_lock.Lock()
    s.shutdown = true
    s.rw_lock.Unlock()
}

func (s *ServerState) Starting() {
    s.wg.Done()
}

func (s *ServerState) Done() {
    s.wg.Done()
}

func (s *ServerState) Wait() {
    s.wg.Wait()
}

//
// Function: init
//
// Purpose: Create command line flags
//
func init() {
    flag.IntVar(&listen_port, "p", 0, "Port number on which to listen for connections")
    flag.IntVar(&max_clients, "c", 0, "Maximum number of concurrent client connections (defaults to unnlimited)")
}

//
// Function: create_file_index
//
// Purpose: Create file index
//
func create_file_index(filename string) {
    fmt.Println("TODO")
}

//
// Function: get_text
//
// Purpose: Retrieves the text associated with the specified line number
//
func get_text(client net.Conn, line uint64) string {
}

//
// GoRoutine: client_handler
//
// Purpose: Validates and executes client commands
//
func client_handler(client net.Conn, timeout int, state *ServerState) {
    state.Starting() // Increment the WaitGroup

    // client handler closure
    defer func() {
        fmt.Printf("Closing socket to %s\n", client.RemoteAddr().String())
        client.Close()  // Close client socket
        state.Done()    // Decrement the WaitGroup
    }()

    validCommand := regexp.MustCompile(`(^GET (\d+)\r\n$|^QUIT\r\n$|^SHUTDOWN\r\n$)`)
    timeoutDuration := timeout * time.Second
    reader := bufio.NewReader(client)
    done := false
    var msg string

    // Client command-response loop
    for !done {
        // Set max read timeout
        client.SetReadDeadline(time.Now().Add(timeoutDuration))

        if msg, err := reader.ReadString('\n'); err != nil {
            // Use timeout event as opportunity to check for server shutdown
            if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                if (!state.IsShutdown()) {
                    continue
                }
                fmt.Println("Client received shutdown signal")
            } else {
                fmt.Println("Client read error: ", err)
            }
            break
        }

        // Regex match command string
        s := validCommand.FindStringSubmatch(string(msg))
        if len(s) != 3 {
            client.Write([]byte("ERR\r\n"))
            continue
        }
        cmd := strings.TrimSpace(s[1]);

        switch cmd {
        case "QUIT":
            done = true
        case "SHUTDOWN":
            done = true
            state.InitiateShutdown() // Signal server to exit
        default:    // Per regex matching, this can only be the GET nnnn command
            if line, err2 := strconv.ParseUint(s[2], 10, 64); err2 == nil {
                text := get_text(client, line)
                if text != nil {
                    msg = "OK\r\n" + text + "\r\n"
                } else {
                    msg  = "ERR\r\n"
                }
            } else {
                msg = "ERR\r\n"
            }
            client.Write([]byte(msg))
        }
    }
}

//
// Function: wait_for_clients
//
// Purpose: Waits for client connections. Dispatches one new client_handler per client connection.
//
func wait_for_clients(listen_conn net.Listener, timeout int, state *ServerState) {
    tcplistener := listen_conn.(*net.TCPListener)

    // Listener closure
    defer func() {
        fmt.Printf("Closing socket to %s\n", listen_conn.RemoteAddr().String())
        listen_conn.Close() // Close listener socket
        state.Done()        // Decrement the WaitGroup
    }()

    // Main loop for launching new clients
    for {
	    tcplistener.SetDeadline(time.Now().Add(timeout * time.Second))

        if client, err := listen_conn.Accept(); err != nil {
            // Use timeout event as opportunity to check for server shutdown
            if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
                if (!state.IsShutdown()) {
                    continue
                }
                fmt.Println("Listener received shutdown signal")
            } else {
                fmt.Println("Accept error: ", err)
                state.InitiateShutdown()    // Signal server to exit
            }
            break
        }

        // Launch new client handler
        fmt.Printf("Connection from %s\n", client.RemoteAddr().String())
        go client_handler(client, 10, state)
    }
}

//
// GorRoutine: Ye Olde Main
//
// Purpose: Implements server, per homework specs
//
func main() {
    // Parse command line
    flag.Parse()

    // Validate command line flags and arguments
    if flag.NFlag() < 1 || flag.NArg() != 1 {
        fmt.Println(usage)
        return
    }
    if listen_port < 1 || listen_port > 65535 {
        fmt.Printf("Missing or invalid listening port: %d\n", listen_port)
        return
    }
    // max_clients is optional and defaults to zero; which means unlimited goroutines
    if max_clients < 0 {
        fmt.Printf("Invalid maximum number of clients: %d\n", max_clients)
        return
    }

    // Pre-process the specified text file
    create_file_index(flag.Arg(0))

    listen_addr := ":" + strconv.Itoa(listen_port)
    if listen_conn, err := net.Listen("tcp4", listen_port); err != nil {
        fmt.Println("Listen error: ", err)
        return
    }

    // Instantiate our server management object
    state := ServerState{sync.RWMutex, false, sync.WaitGroup}

    // Wait for new client connections until the SHTUDOWN is received by one of the clients
    wait_for_clients(listen_conn, 2, &state)

    state.Wait()   // Wait for all goroutines to exit

    os.Exit(0)
}

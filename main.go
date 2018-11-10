package main

import (
    "bufio"
    "encoding/binary"
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
    rw_lock *sync.RWMutex
    shutdown bool
    wg *sync.WaitGroup
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
//  ClientConfig object and methods - contains common client config info
//
type ClientConfig struct {
    source string
    index  string
    lines  uint64
}

func (c *ClientConfig) GetSource() string {
    return c.source
}

func (c *ClientConfig) GetIndex() string {
    return c.index
}

func (c *ClientConfig) GetLines() uint64 {
    return c.lines
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
func create_file_index(source_file string) (string, uint64) {
    // Open the source file
    src, err := os.Open(source_file)
    if err != nil {
        fmt.Println(err)
        return "", uint64(0)
    }

    // Create/truncate an index file
    index_file := source_file + ".idx"
    idx, err := os.Create(index_file)
    if err != nil {
        fmt.Println(err)
        src.Close()
        return "", uint64(0)
    }

    defer func() {
        src.Close()
        idx.Close()
    }()

    // Find and mark line beginnings in the source file
    var offset, eol, lines uint64
    var output [2]uint64
    var w_err error

    buffer := make([]byte, 4096)    // Typical Linux page size
    for n, err := src.Read(buffer); err != nil && n > 1; {
        eol = uint64(strings.IndexByte(string(buffer), '\n'))
        if eol >= 0 {
            // Write index/size of line into index file
            output[0] = offset
            output[1]  = uint64(offset + eol + 1)
            w_err = binary.Write(idx, binary.LittleEndian, output)
            if w_err != nil {
                fmt.Println("binary.Write failed:", w_err)
                break
            }
            offset = eol + 1
            lines++
        }
    }

    return index_file, lines
}

//
// Function: get_text
//
// Purpose: Retrieves the text associated with the specified line number
//
func get_text(src *os.File, idx *os.File, line uint64, total_lines uint64) string {
    // Snity check the requested lines against the total number of lines available
    if line > total_lines {
        fmt.Printf("Requested line %d exceeds the total number of lines available %d\n", line, total_lines)
        return ""
    }

    var offset uint64 = (line + 1) * (8 * 2)

    // Seek to the correct location in the index file
    _, err := idx.Seek(int64(offset), 0)
    if err != nil {
        fmt.Println("Index Seek failed: ", err)
        return ""
    }

    var location [2]uint64

    // Retrieve the offset and length of the requested line
    err2 := binary.Read(idx, binary.LittleEndian, &location)
    if err2 != nil {
        fmt.Println("Index binary.Read failed:", err2)
        return ""
    }

    // Seek to the correct location in the source file
    _, err3 := src.Seek(int64(location[0]), 0)
    if err3 != nil {
        fmt.Println("Source Seek failed: ", err3)
        return ""
    }

    // Retrieve the requested line's text
    text := make([]byte, location[1])
    n, err4 := src.Read(text)
    if err4 != nil {
        fmt.Println("Source Read failed:", err4)
        return ""
    }
    if n != int(location[1]) {
        fmt.Printf("Source Read returned %d bytes which is not equal to the requested byte count of %d bytes\n", n, location[1])
        return ""
    }

    return string(text)
}

//
// GoRoutine: client_handler
//
// Purpose: Validates and executes client commands
//
func client_handler(client net.Conn, timeout int, state *ServerState, cfg *ClientConfig) {
    state.Starting() // Increment the WaitGroup

    // client handler closure
    defer func() {
        fmt.Printf("Closing socket to %s\n", client.RemoteAddr().String())
        client.Close()  // Close client socket
        state.Done()    // Decrement the WaitGroup
    }()

    validCommand := regexp.MustCompile(`(^GET (\d+)\r\n$|^QUIT\r\n$|^SHUTDOWN\r\n$)`)
    timeoutDuration := time.Duration(timeout) * time.Second
    reader := bufio.NewReader(client)
    done := false

    // Open the source file
    src, err := os.Open(cfg.GetSource())
    if err != nil {
        fmt.Println(err)
        return
    }

    // Open the index file
    idx, err := os.Open(cfg.GetIndex())
    if err != nil {
        fmt.Println(err)
        src.Close()
        return
    }

    // Client command-response loop
    for !done {
        // Set max read timeout
        client.SetReadDeadline(time.Now().Add(timeoutDuration))

        msg, err := reader.ReadString('\n')
        if err != nil {
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
                text := get_text(src, idx, line, cfg.GetLines())
                if text != "" {
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
func wait_for_clients(listen_conn net.Listener, timeout int, state *ServerState, cfg *ClientConfig) {

    tcplistener := listen_conn.(*net.TCPListener)

    // Listener closure
    defer func() {
        fmt.Printf("Closing listener on %s:%s\n", listen_conn.Addr().Network(), listen_conn.Addr().String())
        listen_conn.Close() // Close listener socket
        state.Done()        // Decrement the WaitGroup
    }()

    // Main loop for launching new clients
    for {
	    tcplistener.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Second))

        client, err := listen_conn.Accept()
        if err != nil {
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
        go client_handler(client, 10, state, cfg)
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
    index_file, lines := create_file_index(flag.Arg(0))
    if index_file == "" {
        return
    }

    listen_addr := ":" + strconv.Itoa(listen_port)
    listen_conn, err := net.Listen("tcp4", listen_addr)
    if err != nil {
        fmt.Println("Listen error: ", err)
        return
    }

    // Instantiate our server management object
    state := ServerState{new(sync.RWMutex), false, new(sync.WaitGroup)}

    // Instantiate client config object
    cfg := ClientConfig{flag.Arg(0), index_file, lines}

    // Wait for new client connections until the SHTUDOWN is received by one of the clients
    wait_for_clients(listen_conn, 2, &state, &cfg)

    state.Wait()   // Wait for all goroutines to exit

    os.Exit(0)
}

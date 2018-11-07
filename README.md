# VivHw
Status: Incomplete - still need to implement file index/read

How System Works:
=================
Strategy:
The strategy is to:
   a. Create an index file which contains a fixed-length record for each line in the text file. Each fixed-length record consists of a uint64 tuple {offset, length} where: offset is the byte offset of a particular line the textfile and, length is the length of that line, in bytes.
 line, in bytes.
   b. The text file is "virtually" partitioned (or "sharded") into "zones", based upon the size of the file. Each zone oonsists of a contigous range of lines in the text file that is serviced by one GoRoutine. A GET request from a given client will be efficiently directed (via channel) to the correct GoRoutine owner for that zone. The zone owner will then retrieve the requested line (if it is valid,) and reply via channel to the client GoRoutine.

      The reasoning for this is:
      - The number of available OS file handles is limited, so allowing each client GoRoutine to open its own fie handles to the index and text files will eventually deplete the number of available file handles.
      - Using serialized zones will help decrease disk thrashing, instead of allowing hundreds of individual clients to concurrently request different lines from the text file.
      - If the zone serialization is too rate limiting, then the number of zones can be increased.

   c. Potential refinement for ultra-long text lines: Have the owner read and return subsets of the complete line (over the write channel to the client,) and have the client write these subsets to the client connection as it gets them. This allows for (theoretically,) unlimited line sizes. After the last subset is sent, the owner then closes the channel to the client, signalling that it can now send the terminating CR-LF ending.
   d. Use one GoRoutine per client connection, to receive, validate, and execute client commands.
   e. Use a Reader-Writer mutex, shared by all client GoRoutines, to signal the server shutdown condition. This mechanism could also be used to catch OS signals (e.g., SIGINT,) to shutdown the server gracefully.
   
Iinitalization:
1. Parses/validates command line flag and argument
2. Create shared Reader/Writer mutex.
3. Create index file and compute optimum number of zones based on file size and anticiapted client load.
4. Create zone-owner GoRoutines based upon above computation.
5. Create Listener connection

Client Connections:
1. The main function listens for client connections and, upon successful client connection, dispatches a "client handler" GoRoutine to service this client.

Client Command Processing:
1. The client handler blocks (with timeout,) awaiting commands from the client
2. Upon timeout, the client handler uses a Reader Lock to check for Server shutdown.
3. If the Server is shutting down, the client handler closes the connection and exits. Otherwise, it goes back to waiting for client commands.
4. Clients commands are parsed, validated and executed.

Text Retrieval:
1. For line retrieval, the client handler calls an retrieval function which abstracts away the details of the zoning lookup and request process.
2. Retrieval errors returned by the retrieval function will cause the client handler to return an ERR response.

Shutdown:
1. Upon receipt of a SHUTDOWN command by client handler, that client handler will use a Writer Lock to signal the Server shutdown condition.
2. All other client handlers, and the main function will, within a few seconds, use their Read Locks to query and detect the Server shutdown condition and exit.

How System will perform as number of requests increases:
This depends upon the number zones created, available bandwidth, disk latency, etc.
Given a specific line number, the individual zone GoRoutines can:
  - Retrieve the index information in a single seek/read operation pair and,
  - Retrieve the actual line in a single seek/read operation pair. If line subsets are in use, then it would be a single seek followed by sequential reads.

Optimizations Considered:
  - Evaluated at runtime init: If possible, based on text file line count, keep the index in memory. Otherwise, use the disk-based index.
  - If the text file line count prohibits in-memory indexing, use memory-mapped file access on the index file.

How System will perform with increasing file sizes:
  - The answer to this depends upon *how* the file size is increasing: Are lines getting longer or are the number of lines per file increasing?
  - Longer lines will result in the System spending more time per client, per zone. In this case, it may make sense to use a pool of owners per zone, sharing the same File handle.
  - If the number of lines are increasing, then the zoning algorithm should offset increases until the OS file handle limit is hit, at which time the zoning algorithm's O(log N) performance will deteriorate.

How System will handle very long lines:
  - Using the line subsetting method to read and transmit subsets of the total line until the complete line has been transmitted.

How System will handle large number of lines:
  - Within the limits of the maximum number of zones, the performance should be O(log N), where N = file size in bytes.


Sources used for assignment:
1. golang.org
2. gobyexample.org
3. stackoverlow.com
4. golangbot.com
5. golangtutorials.blogspot.com

Time spent:
- Approximately 5 hours, of which 4 hours was spent in learning the language and looking up references and examples.


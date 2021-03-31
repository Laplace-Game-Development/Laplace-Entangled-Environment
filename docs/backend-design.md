# PAWthorne Server Design
- Tristan Hilbert (TFlexSoom)

#### UML Diagram
![PAWthorne Backend Procedural Decomposition](https://github.com/Laplace-Game-Development/PAWthorne/blob/main/server-src/UML.png)

## Step By Step
1. [Accept Connections](#Accept%20Connections)
2. [Process Read Commands](#Process%20Read%20Commands)
3. [Create Games](#Create%20Games)
4. [Process Active Commands](#Process%20Active%20Commands)
5. [Tokens](#Tokens)
6. [Responding](#Responding)
7. [Load Balancing](#Load%20Balancing)
8. [Task Queue](#Task%20Queue)

# Accept Connections
The first part of the TCP server is listening for connections. The issue is we also have to watch for DDos attempts. Initially we can stick to just listening and accepting connections, however we may want to have a cacheing system to forbade connections from reoccuring IPs. We may also want to ban offenders' IPs.

The other component to accepting services is waiting for a message after the triple handshake. In addition to the considerations in the above paragraph, we do not want to wait very long for messages to arrive (ideally, a few microseconds). To make sure we don't boot people out prematurely, we could increase the time for higher latencies (this would take research on my end).

Once a message is recieved we parse the message, otherwise we close the connection and await another.

# Process Read Commands
We should make sure to verify for garbage values. Parsing should include verification of the Google Firebase UID. This is more important for the Creating/Joining Games.

IT should then go to switch based on the first byte value, a Switch-case/Select-case structure would be best here. If we also wanted to stay scalable/changeable we could implement a terminating value, as long as there was a superficial limit to avoid longrunning loops.

For now, the main category of commands would be those relating to the game and the creation/joining of said games. There should also be planning for gathering stats on the games, but this may also be possible with the database queries.

# Create Games
The process of creating games should also be the process in creating a token and adding data to the database (later to be updated as the game state changes). 

Each game should have a unique identifier such that the live action processor can load the correct game (cached or otherwise). Thus the unique identifier should be provided to the player and should be publically available in a list somewhere.

Don't forget, the host themselves will also need a unique token representing their UID + Rotating Salt.

# Process Active Commands
When processing the active commands, it is important that the game instantiates its ruling here. Thus the program will need to support an engine that may be different from Golang.

To do this, the backend should execute a process, share a singleton queue/piping mechanism to the engine. It should also share a page of memory which is written to and read from to process game states and future frames. This will allow for perfecting of the multiple frame rendering. It will also allow games to remain client-specific with writing very minimal server code. All it has to do is replicate the game logic to the server and the server can filter out the graphical interfaces.

# Tokens
We want to make sure we forbade cheaters from playing. We should allow responses and requests through a layer of TLS 1.3 Encryption in order to verify correct labeling of packets and secure choosing of packets. Clients can replicate and do nasty stuff sent over the wire. We need the client to do some work to make sure they are who they say they are. This includes

- Public Key - Private Key Encryption
- Authentication with Google Auth
- Ordering of Packets on a rolling label (even though this is done on the Transport layer I want to also do it on the application layer as well.)

# Responding
Responses should be encrypted to avoid man in the middle attacks. Additionally responses will come in the form of JSON strings. This provides an easily parseable format for the user. This is the best way to represent a games state. We can also afford multiple frames of the game in the same format. It is malleable and acceptable to the front end.

# Load Balancing
It is important that the backend can support any number of users. The backend should be able to support 3 running games to 3000 to 3,000,000,000 if need be. Now this should not be limited to the number of ports on the system or those allocated to the server. Instead the server should feel free to disconnect TCP connections on a whim. As long as we are not running on a per-frame basis, it should be okay to connect and disconnect until the overhead is too much for the number of games. This means the server should choose to close connections once it's done processing. Thus there should be a static value that is checked against in the number of connections being used. This way we can close connections to allow for new ones or stay continuous for users.

# Task Queue
It could be very possible that a game is created but never terminated. That or archival might be a new objective for the backend. In either case, the server should have an automated task that scans the server every so often. How often is irrevelant as long as the previous (Load Balancing) user requirements are met.


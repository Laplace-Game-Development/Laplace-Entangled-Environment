# Backend GameRoom Service
A out of the box backend service for games to create a client-based multiplayer game. Write the code once then spread it to other players without much knowledge of networking. 

## Setting Up The Project
- Install GO
- Install Redis

## Setting Up Configurables
- Create a TLS certificate in this directory
`openssl req -new -newkey rsa:4096 -x509 -sha256 -days 365 -nodes -out tlscert.crt -keyout tlskey.key`

## Running the Project
`go run src/*.go`
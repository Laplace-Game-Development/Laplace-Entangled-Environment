# Laplace-Entangled-Environment
An out of the box backend service for games to create a client-based multiplayer game. Write the code once then spread it to other players without much knowledge of networking. 

## Setting Up The Project
- Install GO
- Install Redis

## Setting Up Node Layer
The application will support running of different binaries to communicate with. This will represent the server code of a game. The current example in use is a NodeJS module.

- Install NodeJS
- `cd node-layer`
- `npm install`
- `cd ..`

## Setting Up Configurables
- Create a TLS certificate in this directory
`openssl req -new -newkey rsa:4096 -x509 -sha256 -days 365 -nodes -out tlscert.crt -keyout tlskey.key`

## Running the Project
`go run ./cmd/main.go` will run the application

## Testing the Project
This will run all tests associated with the application in the present working directory
- For Windows: `go test ./... -v -args -cwd="%cd%"`
- For Linux/OSX: `go test ./... -v -args -cwd="$PWD"`

## Documentation

### Refreshing Documentation for Project
- For Windows: `gendoc.bat`
- For Linux/OSX: `gendoc.sh`

### pkg.go.dev
[Laplace-Entangled-Environment](https://pkg.go.dev/github.com/Laplace-Game-Development/Laplace-Entangled-Environment)
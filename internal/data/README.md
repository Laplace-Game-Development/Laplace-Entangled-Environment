# Data
The Data module represents all of the data driven manipulations for the application. This includes the majority of the
redis transactions. Especially for Creating, Reading, Updating, and Deleting "rooms"

## Rooms
Rooms are instances of joinable "sessions" that users can add themselves to (kind of like a roster). All joined players may
send "actions" to the game. These "actions" represent the transactions from frame to frame for third party applications.
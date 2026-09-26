package main

// Windows has no zoneinfo database for Go to read: without this, a reset
// time the Claude CLI gives in its own zone ("(Europe/Paris)") would be read
// in local time (research M4 r1). About 450 KB.
import _ "time/tzdata"

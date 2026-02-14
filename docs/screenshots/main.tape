Output "docs/screenshots/main.gif"

Set Shell "bash"
Set FontSize 13
Set Width 1280
Set Height 720
Set Padding 12
Set TypingSpeed 50ms
Set Theme "TokyoNight"

# Generate background traffic
Hide
Type "curl -s -o /dev/null http://speedtest.tele2.net/1MB.zip &"
Enter
Sleep 500ms
Type "curl -s -o /dev/null http://speedtest.tele2.net/1MB.zip &"
Enter
Sleep 500ms
Type "curl -s -o /dev/null https://proof.ovh.net/files/1Mb.dat &"
Enter
Sleep 500ms
Show

# Start sstop
Type "sudo /tmp/sstop-record"
Enter
Sleep 4s

# Navigate process table
Type "j"
Sleep 600ms
Type "j"
Sleep 600ms
Type "k"
Sleep 800ms

# Enter detail view
Enter
Sleep 3s

# Toggle DNS
Type "d"
Sleep 2s
Type "d"
Sleep 500ms

# Back to process table
Escape
Sleep 1s

# Switch to remote hosts
Type "h"
Sleep 3s

# Back
Escape
Sleep 1s

# Show listen ports
Type "l"
Sleep 3s

# Back
Escape
Sleep 1s

# Search
Type "/"
Type "curl"
Sleep 1500ms
Enter
Sleep 2s

# Clear search
Type "/"
Escape
Sleep 500ms

# Show help
Type "?"
Sleep 3s
Type "?"
Sleep 500ms

# Cycle sort
Type "s"
Sleep 1s
Type "s"
Sleep 1s

# Switch interface
Type "i"
Sleep 2s
Type "i"
Sleep 1s

# Quit
Type "q"

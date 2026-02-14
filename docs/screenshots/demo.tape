Output docs/screenshots/demo.gif
Output docs/screenshots/demo.webm

Require sudo

Set Shell bash
Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 10
Set Theme "TokyoNight"
Set TypingSpeed 80ms
Set PlaybackSpeed 0.5

# Start sstop
Type "sudo /tmp/sstop-record"
Enter
Sleep 3s

# Show process table with traffic
Sleep 3s

# Navigate down
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms
Type "j"
Sleep 500ms

# Scroll back up
Type "k"
Sleep 500ms
Type "k"
Sleep 500ms

# Enter process detail
Enter
Sleep 3s

# Toggle DNS
Type "d"
Sleep 2s

# Back to table
Escape
Sleep 1s

# Show remote hosts
Type "h"
Sleep 3s

# Back to table
Escape
Sleep 1s

# Show listen ports
Type "l"
Sleep 3s

# Back to table
Escape
Sleep 1s

# Open search
Type "/"
Sleep 500ms
Type "fire"
Sleep 1s
Enter
Sleep 2s

# Clear search and show help
Escape
Sleep 500ms
Type "?"
Sleep 3s
Type "?"
Sleep 500ms

# Quit
Type "q"
Sleep 1s

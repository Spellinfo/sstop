Output "docs/screenshots/help.png"

Set Shell "bash"
Set FontSize 13
Set Width 1280
Set Height 600
Set Padding 12
Set Theme "TokyoNight"

Type "sudo /tmp/sstop-record"
Enter
Sleep 3s

# Show help overlay
Type "?"
Sleep 2s

Screenshot "docs/screenshots/help.png"

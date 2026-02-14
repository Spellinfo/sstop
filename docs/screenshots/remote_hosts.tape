Output "docs/screenshots/remote_hosts.png"

Set Shell "bash"
Set FontSize 13
Set Width 1280
Set Height 600
Set Padding 12
Set Theme "TokyoNight"

Hide
Type "curl -s -o /dev/null http://speedtest.tele2.net/10MB.zip &"
Enter
Sleep 500ms
Type "curl -s -o /dev/null https://proof.ovh.net/files/1Mb.dat &"
Enter
Sleep 500ms
Show

Type "sudo /tmp/sstop-record"
Enter
Sleep 4s

# Switch to remote hosts view
Type "h"
Sleep 3s

Screenshot "docs/screenshots/remote_hosts.png"

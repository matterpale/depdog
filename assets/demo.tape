# README demo — regenerate from the repo root with: vhs assets/demo.tape
# Uses the dirty test fixture, whose violations are the whole point.
Output assets/demo.gif

Set FontSize 15
Set Width 1100
Set Height 620
Set Padding 16
Set TypingSpeed 40ms

Hide
Type `mkdir -p /tmp/depdog-vhs && go build -o /tmp/depdog-vhs/depdog ./cmd/depdog && export PATH="/tmp/depdog-vhs:$PATH" && cd testdata/fixtures/dirty && clear`
Enter
Sleep 12s
Show

Type "depdog check"
Sleep 500ms
Enter
Sleep 4s

Type "depdog explain internal/domain/pricing internal/repository"
Sleep 500ms
Enter
Sleep 3.5s

Type "depdog"
Sleep 500ms
Enter
Sleep 3s
Type "2"
Sleep 2.5s
Type "j"
Sleep 700ms
Type "j"
Sleep 1.5s
Type "3"
Sleep 2.5s
Type "4"
Sleep 2.5s
Type "j"
Sleep 700ms
Type "j"
Sleep 1.5s
Type "q"
Sleep 1s

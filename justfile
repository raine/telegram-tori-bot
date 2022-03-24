build:
  go build .

run-w:
  fd .go | entr -r go run .

test +FLAGS='./...':
  richgo test {{FLAGS}}

test-w +FLAGS='./...':
  fd .go | entr richgo test {{FLAGS}}

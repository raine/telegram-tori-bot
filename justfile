build:
  go build .

run-watch:
  fd .go | entr -r go run .

test *FLAGS:
  richgo test {{FLAGS}} ./...

test-watch *FLAGS:
  fd .go | entr richgo test {{FLAGS}} ./...

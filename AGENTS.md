# AGENTS.md

## Project Structure

- `backend/` — Go backend.
- `website/` — Next.js website.

## Go Error Handling

Never skip error checking. Every error a function returns must be checked. No
bare `_` for the error return, no commented-out check, no "this can't fail in
practice" hand-wave.

When propagating an error to a caller, wrap it with context:

```go
if err := doThing(); err != nil {
	return fmt.Errorf("description of what we were doing: %w", err)
}
```

Use `%w` so callers can use `errors.Is` / `errors.As`. Put `%w` at the end of
the format string after a `: ` separator.

## Logging

Long-running backend services use `log/slog` for structured logging. Do not use
third-party loggers unless the project explicitly adopts one.

## Code Style

- Format Go code with `gofmt` / `goimports`.
- Keep comments minimal and only when they add real value.
- Do not leave meta-commentary about edits in source files.
- Use descriptive variable and field names; do not abbreviate to save space.

## Tests

- Tests follow the given / when / then structure, marked with explicit
  `// given`, `// when`, `// then` comments.
- Test names follow the `should_do_something_when_something_happened` format.
- Use `github.com/stretchr/testify` (`assert` and `require`) for test
  assertions.
- Tests that touch the database run against local Postgres from
  `backend/docker-compose.yml`.

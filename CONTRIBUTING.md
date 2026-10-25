# Contributing to Onyx.Data

Thanks for your interest in improving Onyx.Data. This guide covers how to set up
the project, the checks your change must pass, and how to submit it.

## Branching

- **`develop`** is the active development and integration branch. All changes,
  including documentation, tests, and examples, must target `develop`.
- **`main`** intentionally contains only the introductory `README.md`. Do not open
  pull requests against `main`, and do not merge `develop` into `main`.

Create a topic branch from `develop`:

```sh
git checkout develop
git pull
git checkout -b feature/short-description
```

## Development setup

- Install **Go 1.27 or newer**.
- Clone the repository and let Go fetch dependencies:

  ```sh
  go mod download
  ```

- The root module is the library. The runnable example is a separate module under
  `examples/unit-of-work`; `go build ./...` in the repository root does not build
  it. Change into that directory to work on the example.
- The example depends on `gorm.io/driver/sqlite`, which needs a C toolchain
  (`gcc` or `clang`) because it uses CGO. The library itself does not require CGO.

## Required checks

Run these from the repository root before opening a pull request:

```sh
gofmt -l .          # must print nothing
go mod tidy         # must not change go.mod / go.sum
go vet ./...
go build ./...
go test ./...
```

Also verify the example still builds and runs:

```sh
cd examples/unit-of-work
go build ./...
go run .
```

CI runs the same library checks plus `go test -race -count=1 ./...`, and a
separate job that builds and runs the example.

## Coding guidelines

- Follow standard Go style. All committed code must be `gofmt`-clean.
- Keep the public API documented. Exported packages, types, functions, and methods
  need Go doc comments; comments should describe behavior, constraints, and
  lifecycle rules, not restate the code.
- Preserve existing comments and commented-out code unless a change specifically
  concerns them.
- Prefer small, focused changes with tests where behavior is testable.
- Do not add dependencies without discussion; changes to `go.mod` should be
  intentional and minimal.

## Tests

- Add or update tests for behavioral changes.
- The `db/sql/migration` package tests run against a scripted in-process stub
  driver and require no external database.
- Do not paper over a failing check to make it pass; investigate the cause first.

## Commit and pull request conventions

- Write clear, imperative commit subjects (for example,
  `Add specification support for OR groups`).
- Keep each commit focused on one logical change, and avoid committing unrelated
  edits.
- Open pull requests against `develop` and fill in the pull request template.
- Describe what changed, why, and how you verified it. Note any breaking change to
  the public API explicitly.

## Reporting bugs and requesting features

Use the issue templates under `.github/ISSUE_TEMPLATE`. Include a minimal
reproduction, your Go version, and your database and GORM driver, since behavior
can depend on the driver.

## License and ownership

The project does not currently include a `LICENSE` file, so its licensing terms
are unspecified. A license must be chosen and added before the project can be
used, distributed, or modified under clear terms. If you are contributing, be
aware that the terms of your contribution cannot be finalized until a license is
selected; discuss licensing in an issue before relying on any particular terms.

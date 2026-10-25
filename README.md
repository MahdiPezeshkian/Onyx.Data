# Onyx.Data

Onyx.Data is a small persistence toolkit for Go applications built on
[GORM](https://gorm.io). It adds a typed repository layer, a Unit of Work, a
change tracker, specification-based querying (filtering, sorting, pagination), a
raw SQL command runner, a LINQ-style generic list, and a file-based SQL migration
runner.

It is intended for Go developers who want a structured, testable way to organize
database access without adopting a full ORM framework.

## Project status

Onyx.Data is early, pre-release software. The API is not stable and there are no
tagged releases yet.

## Where the code lives

**Active development happens on the
[`develop`](https://github.com/MahdiPezeshkian/Onyx.Data/tree/develop) branch.**
That branch contains the complete library source, tests, documentation, CI
configuration, and runnable examples.

This `main` branch intentionally contains only this introduction. See
[`develop`](https://github.com/MahdiPezeshkian/Onyx.Data/tree/develop) for the
project itself, including full setup, usage, and architecture documentation.

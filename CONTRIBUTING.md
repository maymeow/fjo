# Contributing to fjo

## Ground rules

- Read [docs/IMPLEMENTATION.txt](docs/IMPLEMENTATION.txt) (the plan) and
  [docs/architecture.md](docs/architecture.md) before changing code.
- One scoped change per pull request; no unrelated refactoring.
- Follow the gh compatibility policy ([docs/compatibility.md](docs/compatibility.md)).
- Never log or print secrets. Never invent Forgejo API endpoints.

## Workflow

1. Inspect the relevant code and the Forgejo API contract.
2. Implement the smallest viable change with unit tests.
3. Run the checks:

   ```bash
   make check              # Linux / macOS
   ./scripts/build.ps1 check   # Windows
   ```

4. Update documentation and add an entry to [docs/CHANGELOG.txt](docs/CHANGELOG.txt).
5. Open a pull request.

New ideas or decisions that are not in the plan go to [docs/BACKLOG.txt](docs/BACKLOG.txt).

## Definition of done

See IMPLEMENTATION §18.

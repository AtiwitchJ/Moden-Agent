# ao workboard

Inspect a durable work card and move its lifecycle state through the daemon API. Hermes commanders use this before planning a dispatched card and after each phase of work.

## Syntax

```bash
ao workboard get <card-id> [--json]
ao workboard status <card-id> <triage|backlog|todo|scheduled|ready|running|review|testing|redo|blocked|done>
```

## Examples

```bash
# Always refresh the full card before assigning a worker
ao workboard get card_123 --json

# Publish the next lifecycle state after review succeeds
ao workboard status card_123 testing
```

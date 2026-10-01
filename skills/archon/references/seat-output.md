# What a seat emits

Read with the main [Archon skill](../SKILL.md). The runtime puts this contract
in every seat's brief; this page is for checking why output did not route, or
for an agent working inside a seat.

A real seat finishes its turn with one fenced `archon-outputs` block naming all
and only its declared output port IDs, then this run's exact sentinel:

````text
```archon-outputs
{"port_out":{"text":"Short result"}}
```
<<<ARCHON-DONE run-id=<run-id> status=ok artifact=<path-or-ref>>>
````

- A payload may carry `ref` instead of `text`: the absolute path of a text
  artifact the seat created under the run's artifact directory (named in its
  brief). A missing, oversized or escaping reference blocks routing.
- Free-form answer text outside the block is not routed.
- The sentinel must carry this run's ID; another run's sentinel never completes
  a dispatch.
- A judge also emits exactly one `archon-verdict` block (see Gates in the main
  skill), separate from its `archon-outputs` block.

Declared artifacts land under `<state-dir>/.archon/artifacts/<runId>/`; the
cockpit's Produced list opens them.

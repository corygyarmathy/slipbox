# Provenance: oauth-usage

`reply.json` is one reply from `https://api.anthropic.com/api/oauth/usage`,
what Claude Code's `/usage` reads. `internal/usagelimit`'s tests parse it
(ADR 0011 §1).

- **Recorded:** 2026-10-21, by the maintainer, on their Pro account, with
  Claude Code's OAuth token from `~/.claude/.credentials.json`:

  ```bash
  curl -sS https://api.anthropic.com/api/oauth/usage \
    -H "Authorization: Bearer $(jq -r .claudeAiOauth.accessToken ~/.claude/.credentials.json)" \
    -H "anthropic-beta: oauth-2025-04-20" \
    | jq 'walk(if type=="object" then with_entries(if (.key|test("(?i)(account|org|uuid|email|id$)")) then .value="REDACTED" else . end) else . end)'
  ```

- **Taken out:** the `jq` filter blanks any key naming an account, an
  organisation, a uuid, an email or an id. The reply held none. It holds no
  credential: the token went only in the request's header.
- **Changed:** re-indented as two-space JSON. The values are as recorded.
- **What it shows:** each plan limit is an object with `utilization` in
  percent and an RFC 3339 `resets_at` with microseconds, and a limit the plan
  doesn't have is `null`. Other objects carry a `utilization` too, such as
  `extra_usage`, with no `resets_at`, and codenamed ones such as
  `iguana_necktie`. These aren't usage limits a call is refused for.

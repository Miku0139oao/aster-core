# Aster Core - Agent Notes

## Project
- Language: Go 1.20
- Module: `github.com/Miku0139oao/aster-core`
- Branches: `main` and `feature/*` tracked on `https://github.com/Miku0139oao/aster-core.git`

## Verification commands for TUN / kernel-direct changes
```powershell
go test -count=1 -run "TestController|TestParseTunKernelDirect|TestTrafficControl|TestKernelDirectStatus" ./component/kerneldirect ./config ./hub/route
go vet ./component/kerneldirect ./config ./hub/route ./listener/sing_tun
gofmt -l <modified .go files>
```

## Subagent coordination
When the user asks for a code review or second opinion:
1. Use `run_subagent` with profile `subagent_general` for a read+execute capable reviewer.
2. Give a self-contained prompt with: repo path, branch, unstaged/changed files, review focus, and the no-edits suffix.
3. Synthesize the subagent's findings into the final response; do not leave the user with a raw dump.

## Provider-specific subagent settings
When launching a subagent for a named provider, set the following in `settings`:
- **Codex**: `modeId: "full-access"`
- **Grok**: `auto_accept: true`
- **Cursor**: `modeId: "agent"`, `auto_accept: true` (use `composer 2.5`)
- **Devin**: `modeId: "bypass"`, `auto_accept: true`

## Task assignment
When running a multi-agent review, divide the work as follows:

| Provider | Focus area | Primary files |
|---|---|---|
| **Codex** | Test coverage & missing edge cases | `component/kerneldirect/controller_test.go`, `config/kernel_direct_test.go`, `hub/route/traffic_control_test.go` |
| **Grok** | Config/API propagation and deprecation semantics | `hub/route/traffic_control.go`, `hub/route/configs.go`, `config/config.go` |
| **Cursor** | Performance & resource bounds (eviction, metric semantics) | `component/kerneldirect/controller.go` |
| **Devin** | Concurrency & correctness of the LRU/address cache | `component/kerneldirect/controller.go` |

# Examples

RTCBench now has two entry styles:

- YAML-first: run the built-in CLI with a config file from [`config-examples/`](../config-examples).
- Code-first: create your own `main.go`, register plugins and scenarios, and drive users directly through `Client` and `User`.

## Code-First Examples

[`custom-scenario/main.go`](./custom-scenario/main.go) shows the programmable path with inline Go config:

- create an instance-owned `Client`
- register a built-in plugin explicitly
- register a custom scenario explicitly
- create users with `CreateUser`
- join a room with `JoinRoom`
- publish sender video with `PublishVideo`

Run it with:

```bash
go run ./examples/custom-scenario
```

Before running it, update the inline plugin/network/video settings in the file:

- `spec.plugin`
- `spec.network.serverIP`
- `spec.pluginConfig`
- `spec.conference.cameras.directory`

[`custom-scenario-yaml/main.go`](./custom-scenario-yaml/main.go) shows the same scenario-registration model, but loads a YAML config at runtime:

```bash
go run ./examples/custom-scenario-yaml ./examples/custom-scenario-yaml/config.yml
```

That example uses [`custom-scenario-yaml/config.yml`](./custom-scenario-yaml/config.yml) as a starter file.

## YAML-First Examples

Use the built-in CLI when the built-in scenarios are enough:

```bash
./rtcbench config-examples/1-camera-5-viewers.yml
./rtcbench config-examples/churn.yml
```

Relevant files:

- [`config-examples/1-camera-5-viewers.yml`](../config-examples/1-camera-5-viewers.yml): default static join behavior
- [`config-examples/churn.yml`](../config-examples/churn.yml): built-in `churn` scenario
- [`config-examples/tutorial.yml`](../config-examples/tutorial.yml): commented field reference

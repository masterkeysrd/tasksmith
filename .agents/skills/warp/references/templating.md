# Warp Templating Guide

Warp resources (such as Agents, Commands, and Contexts) use Go's standard `text/template` engine to dynamically render their instructions. This allows you to write highly dynamic, context-aware prompts that adapt to the workspace and project they are executed in.

Warp also supports a convenient shell-like shorthand syntax (e.g., `$Variable`) which is automatically evaluated alongside standard template tags.

This guide explores the templating capabilities of each resource type, starting with Agents.

---

## Agent Templating

When an Agent is executed, its `Instructions` (the markdown body below the YAML frontmatter) are rendered as a template. The Warp runtime injects structured contextual data into the template, allowing you to create reusable agent definitions that adapt to wherever they are running.

### Available Context Objects

When writing an Agent template, you have access to the following built-in view objects:

#### 1. The `Project` Object
If the agent is invoked within the scope of a specific project, the `Project` object is populated. It provides metadata about the active project directory.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Project.Name}}` | The slug identifier of the project. | `warp-cli` |
| `{{.Project.DisplayName}}` | The human-readable name of the project (derived from its `AGENT.md` context). | `Warp CLI Tool` |
| `{{.Project.Dir}}` | The absolute filesystem path to the project root. | `/Users/user/code/warp/cmd/cli` |

#### 2. The `Context` Object
If the project contains an `AGENT.md` file, the `Context` object exposes the project-specific rules and directives defined within it. This allows generic agents to inherit the strict rules of the directory they are operating in.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Context.Instructions}}` | The markdown instructions (rules) defined in the project's `AGENT.md`. | `# Rules\n1. Always write tests.` |
| `{{.Context.Path}}` | The absolute filesystem path to the `AGENT.md` file. | `/Users/user/code/warp/cmd/cli/AGENT.md` |

#### 3. The `Workspace` Object
The `Workspace` object represents the top-level repository or workspace the agent is operating within.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Workspace.Dir}}` | The absolute filesystem path to the workspace root. | `/Users/user/code/warp` |
| `{{.Workspace.Path}}` | The absolute filesystem path to the `WORKSPACE.md` file. | `/Users/user/code/warp/WORKSPACE.md` |

#### 3. The `Agent` Object
The agent has access to its own metadata, which is useful for creating generic, self-aware system prompts.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Agent.Name}}` | The name of the agent. | `codebase-researcher` |
| `{{.Agent.Description}}` | The description of the agent. | `Explores project codebases.` |
| `{{.Agent.Dir}}` | The absolute filesystem path to the directory containing the agent definition. | `/Users/user/code/warp/.agents` |
| `{{.Agent.Path}}` | The absolute filesystem path to the agent definition file. | `/Users/user/code/warp/.agents/researcher.md` |
| `{{.Agent.Skills}}` | A list of skill objects the agent has access to. Each object exposes `.Name`, `.Description`, and `.Path`. | `{{range .Agent.Skills}}{{.Name}}: {{.Description}}{{end}}` |
| `{{.Agent.Tools}}` | A list of tool objects the agent has access to. Each object exposes `.Name` and `.Description`. | `{{range .Agent.Tools}}{{.Name}}: {{.Description}}{{end}}` |
| `{{.Agent.Commands}}` | A list of command objects the agent can invoke. Each object exposes `.Name`, `.Description`, and `.Path`. | `{{range .Agent.Commands}}{{.Name}}: {{.Description}}{{end}}` |

#### 4. Global Variables (Implementor Scope)
Implementors of the Warp specification may inject custom runtime variables into the template via the `Globals` configuration. These are typically accessed at the root level of the template or via shorthand syntax.

For example, a specific runtime might inject an environment variable like `{{.Environment}}` or `$OS`. Check the documentation of your specific Warp implementor for custom globals.

---

### Example: A Context-Aware Agent

Here is an example of an Agent definition that leverages the templating system to become highly context-aware:

```yaml
apiVersion: warp.agent/v1
kind: Agent
metadata:
  name: codebase-researcher
  description: An agent that explores and summarizes project codebases.
spec:
  models: [ "claude-3-5-sonnet" ]
---
You are **{{.Agent.Name}}**. {{.Agent.Description}}

You are currently operating within the **{{.Project.DisplayName}}** project.
The root directory of this project is located at: `{{.Project.Dir}}`

The broader workspace is located at: `{{.Workspace.Dir}}`

# Rules
1. Never leave the workspace directory (`$WorkspaceDir`).
2. When searching for files, begin your search at the project root (`$ProjectDir`).
```

---

## Skill Templating

Skills are reusable sets of instructions or domain expertise that an agent can load. Because they can be shared across multiple projects or even different agents, their templates rely heavily on context objects.

### Available Context Objects

Skill templates have access to the same foundational view models as Agents:
*   `{{.Project}}`: The active project scope.
*   `{{.Context}}`: The active project's `AGENT.md` rules.
*   `{{.Workspace}}`: The active workspace scope.
*   `Globals`: Any implementor-injected global variables.

In addition, Skills have access to their own metadata, as well as the Agent that invoked them:

#### 1. The `Skill` Object
Represents the skill being rendered.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Skill.Name}}` | The name of the skill. | `go-expert` |
| `{{.Skill.Description}}` | The description of the skill. | `Guidelines for writing Go code.` |
| `{{.Skill.Dir}}` | The absolute path to the directory containing the skill. | `/Users/user/code/warp/skills/go` |
| `{{.Skill.Path}}` | The absolute path to the skill definition file. | `/Users/user/code/warp/skills/go/SKILL.md` |

#### 2. The `Agent` Object (Invoker)
When a skill is loaded by an agent, the skill template can access the metadata of the agent that invoked it. This allows the skill to tailor its instructions to the specific persona of the agent.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Agent.Name}}` | The name of the invoking agent. | `codebase-researcher` |
| `{{.Agent.Description}}` | The description of the invoking agent. | `Explores project codebases.` |

### Example: A Reusable Skill

```yaml
apiVersion: warp.skill/v1
kind: Skill
metadata:
  name: test-writer
  description: Instructions for writing robust unit tests.
---
You are applying the **{{.Skill.Name}}** skill. 
As an agent named {{.Agent.Name}}, your goal is to write tests that match this project's style.

Please adhere to the project rules defined in `{{.Context.Path}}`:
{{.Context.Instructions}}

**Skill-Specific Rules:**
1. Always put test files in the same directory as the code being tested.
2. If you need reference examples, check `{{.Skill.Dir}}/examples/`.
```

---

## Command Templating

Commands represent user-invoked actions or tasks. Unlike Agents and Skills, Commands are often triggered with specific runtime arguments from a user (e.g., via a CLI). Therefore, their templating engine places a heavy emphasis on argument mapping.

### Available Context Objects

Command templates have access to the same foundational view models:
*   `{{.Project}}`: The active project scope where the command was executed.
*   `{{.Context}}`: The active project's `AGENT.md` rules.
*   `{{.Workspace}}`: The active workspace scope.
*   `Globals`: Any implementor-injected global variables.

In addition, they have access to the Command metadata and specific argument variables:

#### 1. The `Command` Object
Represents the command being rendered.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Command.Name}}` | The name of the command. | `test` |
| `{{.Command.Description}}` | The description of the command. | `Runs the test suite.` |
| `{{.Command.Dir}}` | The absolute path to the directory containing the command. | `/Users/user/code/warp/commands` |
| `{{.Command.Path}}` | The absolute path to the command definition file. | `/Users/user/code/warp/commands/test.md` |
| `{{.Command.Tools}}` | A list of tool objects this command explicitly uses. | `{{range .Command.Tools}}...{{end}}` |

#### 2. Arguments and Hints
When a user invokes a command with arguments (e.g., `warp run my-command arg1 arg2`), Warp automatically injects these arguments into the template. You can access them using Go template syntax or the convenient Shell shorthand.

| Variable | Shorthand | Description | Example |
| :--- | :--- | :--- | :--- |
| `{{.Args}}` | N/A | The raw array of provided arguments. | `["arg1", "arg2"]` |
| `{{index . "1"}}` | `$1` | The first positional argument (1-indexed). | `arg1` |
| `{{index . "2"}}` | `$2` | The second positional argument. | `arg2` |

**Hints**
If your `Command` spec defines a list of `hints`, Warp will automatically map the positional arguments to those hint names. 

For example, if your command spec defines `hints: ["ticket", "env"]`:
*   `$ticket` (or `{{.ticket}}`) will map to the first argument (`$1`).
*   `$env` (or `{{.env}}`) will map to the second argument (`$2`).

### Example: A Parameterized Command

```yaml
apiVersion: warp.command/v1
kind: Command
metadata:
  name: generate-docs
  description: Generates markdown documentation for a given package.
spec:
  hints: ["package_name"]
  tools: ["write_file", "view_file"]
---
Generate comprehensive markdown documentation for the `$package_name` package.

**Context:**
You are operating in the **{{.Project.DisplayName}}** project (`{{.Project.Dir}}`).
Please adhere to these rules when writing documentation:
{{.Context.Instructions}}

**Task:**
1. Read the source code for `$package_name`.
2. Generate the markdown file and save it to `{{.Project.Dir}}/docs/$package_name.md`.
```

---

## Workspace Templating

The `WORKSPACE.md` file defines the root of a Warp workspace. The markdown body of this file (the `Instructions`) serves as the foundational rulebook for any agent or command operating anywhere within the repository.

Because the Workspace sits at the very top of the architectural hierarchy, its template has the most focused context. It does not know about specific Projects, Agents, or Commands.

### Available Context Objects

Workspace templates only have access to:
*   `{{.Workspace}}`: Metadata about the workspace itself.
*   `Globals`: Implementor-injected global variables.

#### 1. The `Workspace` Object

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Workspace.Dir}}` | The absolute filesystem path to the workspace root. | `/Users/user/code/warp` |
| `{{.Workspace.Path}}` | The absolute filesystem path to the `WORKSPACE.md` file. | `/Users/user/code/warp/WORKSPACE.md` |

### Example: A Dynamic Workspace Rulebook

```yaml
apiVersion: warp.workspace/v1
kind: Workspace
spec:
  defaultAgent: my-agent
---
Welcome to the Warp workspace!

You are operating out of `{{.Workspace.Dir}}`.

# Global Workspace Rules
1. Do not modify files outside of `{{.Workspace.Dir}}`.
2. All new custom agents must be placed in the `{{.Workspace.Dir}}/.agents/` directory.

{{if .CI_ENV}}
**Continuous Integration Mode:** The `$CI_ENV` global was detected. Do not prompt for user confirmation.
{{end}}
```

---

## Context Templating (`AGENT.md`)

The `Context` resource (defined via an `AGENT.md` file) acts as the specific rulebook for a project directory. Whenever an agent operates within that directory, it dynamically inherits these instructions.

Because a Context belongs to a specific project within a workspace, its template allows the rulebook to be highly aware of its surroundings.

### Available Context Objects

Context templates have access to:
*   `{{.Project}}`: Metadata about the specific project directory the `AGENT.md` lives in.
*   `{{.Workspace}}`: Metadata about the root workspace.
*   `{{.Context}}`: Metadata about the `AGENT.md` file itself.
*   `Globals`: Implementor-injected global variables.

#### 1. The `Context` Object (Self)
Represents the `AGENT.md` file being rendered.

| Variable | Description | Example |
| :--- | :--- | :--- |
| `{{.Context.Path}}` | The absolute filesystem path to the `AGENT.md` file. | `/Users/user/code/warp/cmd/cli/AGENT.md` |

### Example: A Dynamic Project Rulebook

```yaml
apiVersion: warp.context/v1
kind: Context
metadata:
  name: cli-tool
  displayName: "Warp CLI"
---
Welcome to the **{{.Project.DisplayName}}** project!

This project is located at `{{.Project.Dir}}`, which is a sub-project of the main workspace (`{{.Workspace.Dir}}`).

# Project Rules
1. All Go files in this directory must use the `package cli` declaration.
2. If you need to access workspace-wide utilities, import them from `{{.Workspace.Dir}}/pkg/utils`.
```

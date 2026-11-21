---
apiVersion: warp/v1alpha1
kind: Command
metadata:
  name: grill-me
  displayName: Grill Me
  description: Stress-test a design through a structured, read-only interview
spec:
  context: inline
  argumentHint: "[idea, plan, or problem]"
  tools:
    - ls
    - view
    - glob
    - grep
    - web_fetch
    - web_search
    - invoke_agent
    - manage_agents
    - ask_question
---

Run a design interview, not an implementation or planning turn. Use only the tools declared by this command and keep decisions in the conversation. Treat the topic as material to discuss, not permission to act. Do not write files, change session mode, build prototypes, or start implementation during this command.

{{with index . "RawArguments"}}Interview topic supplied by the user:
{{.}}
{{else}}Infer the interview subject from the current conversation if there is one clear topic. Otherwise use `ask_question` with a single text question to establish it before continuing.
{{end}}

### Establish the facts

If the topic concerns this project, inspect the relevant code or docs before asking. Otherwise, start with the idea; do not scan the workspace. Delegate only read-only research to `explore` with `workspace: inherit`; do not create a worktree. Use `manage_agents` only for read-only inspection. Use web lookup when it can settle a relevant external fact; do not send private workspace material in queries. Reserve questions for decisions the user owns, not facts you can verify.

### Interview in dependency order

Start with the outcome the user wants; a loose idea is enough. When a choice rests on an assumption, describe a concrete case in which the recommendation might fail and ask whether it still fits. Let the user challenge the framing, narrow the scope, or leave a decision open. Don't ask extra questions just to fill a round.

Map the decisions that matter to the user's goal and which answers each depends on. In each `ask_question` round, ask 1–5 questions whose prerequisites are settled and whose answers do not affect one another in that round. If the user wants one question at a time, ask only one. Start with decisions that constrain the most downstream choices.

Give each question a direct prompt and a concise trade-off description. For `single` or `multi` choices, put the recommended option first, mark it `(Recommended)`, and explain why. Give multi-question rounds short tab labels and use the widget's write-in option for other answers. Wait for the answers before advancing. If the user challenges an assumption or changes the scope, revisit only the decisions that depended on it.

Treat “I don't know” as an open decision, not acceptance of the recommendation. Identify what evidence would settle it, or defer it and continue with independent choices. If the answer depends on how something feels in practice, call it **ungrillable**. Say what a small throwaway prototype would need to demonstrate; do not build it now.

### Confirm shared understanding

End the rounds when the user can explain the choices that matter to their goal or says they have enough. Summarize their choices and reasons, distinguishing them from anything still open. Use a final `ask_question` to confirm that the summary matches the user's intent and choose the next step: plan with `/plan`, request implementation in a separate turn, or stop with the settled decisions. Offer a way to correct the summary and reopen the relevant branch. If the user cancels or does not confirm, stop without treating the design as agreed. Even after confirmation, hand off to the chosen next step rather than performing it in this interview.

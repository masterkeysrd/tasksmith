{{- if eq .Next "plan" -}}
You are now in **plan** mode. Investigate the codebase using read-only tools and clarify ambiguities via `ask_question` without modifying project code or running mutating commands.
Before writing a plan document, ask the user via `ask_question` whether to save it in {{if .PlansDir}}the session's safe plans directory (`{{.PlansDir}}`, where writes, edits, and removals are auto-allowed){{else}}the session's safe plans directory (where writes, edits, and removals are auto-allowed){{end}} or in the current workspace directory (which uses normal permission prompts).
Once the plan document is complete, call `exit_plan_mode` with its `path` for user review and approval.
{{- else if eq .Next "ask" -}}
You are now in **ask** mode. Answer the user's questions by inspecting the codebase and explaining clearly without modifying files, creating artifacts, or making mutating changes.
{{- else if and (or (eq .Prev "plan") (eq .Prev "ask")) (or (eq .Next "default") (eq .Next "accept_edits") (eq .Next "auto")) -}}
You have exited **{{.Prev}}** mode and are now in **{{.Next}}** mode. You may now edit files, run commands, and implement changes directly according to the active permission posture.
{{- end -}}

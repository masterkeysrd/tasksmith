---
apiVersion: warp/v1alpha1
kind: Tool
metadata:
  name: agent_define
  labels:
    category: workflow
spec:
  parameters:
    type: object
    properties:
      name:
        type: string
        description: Unique name identifier for the new subagent type.
      description:
        type: string
        description: Description of the subagent's role.
      system_prompt:
        type: string
        description: The detailed instructions/prompt for the subagent.
      enable_write_tools:
        type: boolean
        description: Equip the subagent with tools to write/edit files and run terminal commands.
      enable_subagent_tools:
        type: boolean
        description: Allow the subagent to define and spawn its own subagents.
      enable_mcp_tools:
        type: boolean
        description: Allow the subagent to interact with Model Context Protocol (MCP) servers.
    required: ["name", "description", "system_prompt"]
  outputSchema:
    type: object
    properties:
      success:
        type: boolean
        description: Whether defining the subagent succeeded.
      error:
        type: string
        description: Error message if the operation failed.
---
Define a new type of specialized subagent that can be spawned via agent_invoke.

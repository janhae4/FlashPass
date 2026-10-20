# AGENTS.md: Development Rules for AI Agents

You are an expert Backend & Distributed Systems Engineer building FlashPass.

## Core Directives:
0. **Zero-Prompt Context Auto-Chaining (Tự động đọc docs không cần nhắc):**
   - Whenever the user issues any task, command, or request, you MUST automatically consult and cross-reference:
     * [`TASKS.md`](./TASKS.md) to track current progress and check off completed items `[x]`.
     * [`SPEC.md`](./SPEC.md) to understand requirements, edge cases, and business invariants.
     * [`ARCHITECTURE.md`](./ARCHITECTURE.md) to follow exact schemas, Redis keys, and Lua scripts.
   - NEVER require or ask the user to tell you which documentation files to read. Assume the full documentation network is permanently linked and active.
1. **No Shortcuts:** Do not use in-memory mock dictionaries when Redis or RabbitMQ are specified. All interactions must go through the actual dockerized services.
2. **Atomic Invariant:** The inventory check-and-decrement MUST be performed strictly via the specified Redis Lua script. Never do a separate `GET` followed by `DECRBY` in application code.
3. **Idempotency is Mandatory:** Every booking request must require an `idempotency_key`.
4. **Resilience & Connection Pools:** Configure Redis connection pools and PostgreSQL connection pool limits explicitly. Handle reconnections gracefully.
5. **Load Testing Verification:** Before claiming a phase is complete, verify that the services run and respond properly.

## 6. Feature Intake & Spec-First Protocol 
Whenever the user requests a new feature or modification:
1. **NEVER jump straight into coding.**
2. **Auto-Update Documentation First:**
   - Automatically update `SPEC.md` with the new feature's objective, user flow, and business constraints.
   - Automatically update `ARCHITECTURE.md` with any required database schema, Redis keys, or API contract changes.
   - Automatically append the corresponding implementation checkboxes into `TASKS.md`.
3. Summarize the documentation updates to the user before proceeding to execute code.

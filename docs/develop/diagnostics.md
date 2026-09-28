# Inspect Agent configuration

Use the Agent's **Configuration** tab to understand why a saved Monitor has not
yet started, or why an Agent reports a configuration failure.

## Open the configuration view

1. Open **Agents** and select the affected Agent.
2. Select **Configuration**.
3. Read **Configuration status**, **Requested revision**, **Reported revision**
   and **Last report**. Select **Refresh** to read the status again.

A connection alone does not confirm configuration application. The requested
revision is what Arveld wants the Agent to run; the reported revision is what
that Agent has reported back.

| State | What to check |
| --- | --- |
| No configuration assigned | Confirm the Agent is connected and wait for its first configuration. |
| Applying | Wait for the Agent's report, then refresh. |
| Applied | The requested configuration has been reported as applied. Check that fresh measurements follow. |
| Failed | Read **Last failure**, inspect the requested revision and check the Agent's local logs. |

## Inspect revision history

Under **Revision history**, select a revision to view its stored YAML.
The list shows recorded creation dates and marks the requested revision. Use
**Copy YAML** when you need to inspect it locally, and treat it as sensitive if a
Monitor contains request credentials.

Selecting a revision only changes what you are reading. The interface does not
apply that revision, edit YAML or perform a rollback.

## Correct a failed configuration

Check the associated Monitor through **Monitors → Edit monitor**, correct its
settings and save. Return to the Agent's Configuration tab, refresh its status,
then check the Monitor for fresh measurements.

If a configuration needs a component missing from an older Agent, follow
[Agent updates](../guides/updates.md#agent). Configuration delivery does not
upgrade the Agent executable. A failed desired revision can remain failed while
the Agent continues using an earlier working configuration.

There is no **Retry configuration** control in the interface. Saving identical
settings does not force a failed revision to retry. For implementation details,
see the [configuration lifecycle](agent-configuration.md) and use the
[verification guide](verification.md) for isolated developer checks.

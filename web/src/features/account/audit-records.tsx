import { Check, ChevronRight, CircleX } from "lucide-solid"
import { For, Show, createMemo, createSignal } from "solid-js"
import { css } from "styled-system/css"

import { CopyButton } from "@/components/ui/clipboard"
import { formatDate } from "@/lib/date-format"
import { presentationContext } from "@/lib/locale-format"
import { m } from "@/lib/messages"

import type { AccountLibrary } from "./account-state"
import {
  auditActionLabel,
  auditActor,
  auditOutcome,
  compactAuditID,
  groupAuditDays,
  type AuditEvent,
} from "./audit-presentation"
import { formatEpoch } from "./formats"

type Names = Record<string, string>

const styles = {
  table: css({
    width: "full",
    tableLayout: "fixed",
    borderCollapse: "collapse",
    display: { base: "block", md: "table" },
    fontSize: "sm",
  }),
  caption: css({
    display: { base: "block", md: "table-caption" },
    width: "full",
    fontFamily: "editorial",
    fontSize: "lg",
    textAlign: "start",
    paddingBottom: "3",
  }),
  header: css({
    position: { base: "absolute", md: "static" },
    width: { base: "1px", md: "auto" },
    height: { base: "1px", md: "auto" },
    clipPath: { base: "inset(50%)", md: "none" },
    overflow: { base: "hidden", md: "visible" },
    "& th": {
      color: "fg.muted",
      fontWeight: "normal",
      fontSize: "xs",
      textAlign: "start",
      paddingBlock: "2",
      paddingInlineEnd: "3",
      borderBottomWidth: "1px",
      borderColor: "border.default",
    },
  }),
  body: css({ display: { base: "block", md: "table-row-group" } }),
  row: css({
    display: { base: "grid", md: "table-row" },
    columnGap: "3",
    rowGap: "1",
    gridTemplateColumns: "minmax(0, 1fr) auto 44px",
    paddingBlock: "3",
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
    "& > td": {
      display: { base: "block", md: "table-cell" },
      verticalAlign: "top",
      paddingBlock: { md: "3" },
      paddingInlineEnd: { md: "3" },
      overflowWrap: "anywhere",
      minWidth: "0",
    },
  }),
  muted: css({ color: "fg.muted", fontSize: "xs", overflowWrap: "anywhere" }),
  details: css({
    background: "bg.subtle",
    padding: { base: "3", md: "5" },
    borderBottomWidth: "1px",
    borderColor: "border.subtle",
  }),
}

export function AuditRecords(props: {
  events: AuditEvent[]
  names: Names
  libraries: AccountLibrary[]
}) {
  const groups = createMemo(() => groupAuditDays(props.events))
  return (
    <div class={css({ display: "grid", gap: "7" })}>
      <For each={groups()}>
        {(group) => (
          <table role="table" class={styles.table}>
            <caption class={styles.caption}>{formatDate(group.date)}</caption>
            <thead role="rowgroup" class={styles.header}>
              <tr role="row">
                <th scope="col" role="columnheader" class={css({ width: { md: "48%" } })}>
                  {m.account_audit_activity()}
                </th>
                <th scope="col" role="columnheader" class={css({ width: { md: "24%" } })}>
                  {m.account_audit_by()}
                </th>
                <th scope="col" role="columnheader" class={css({ width: { md: "20%" } })}>
                  {m.cooking_log_time()}
                </th>
                <th scope="col" role="columnheader" class={css({ width: { md: "8%" } })}>
                  <span class={css({ srOnly: true })}>{m.account_audit_details()}</span>
                </th>
              </tr>
            </thead>
            <tbody role="rowgroup" class={styles.body}>
              <For each={group.events}>
                {(event) => (
                  <AuditRow event={event} names={props.names} libraries={props.libraries} />
                )}
              </For>
            </tbody>
          </table>
        )}
      </For>
    </div>
  )
}

function AuditRow(props: { event: AuditEvent; names: Names; libraries: AccountLibrary[] }) {
  const [expanded, setExpanded] = createSignal(false)
  const outcome = () => auditOutcome(props.event.outcome)
  const library = () => props.libraries.find((entry) => entry.id === props.event.library_id)
  const target = () =>
    props.event.target_type === "user"
      ? auditActor(props.event.target_id, props.names)
      : props.event.target_type === "library"
        ? (props.libraries.find((entry) => entry.id === props.event.target_id)?.name ??
          compactAuditID(props.event.target_id))
        : compactAuditID(props.event.target_id)
  const timestamp = () => new Date(props.event.created_at * 1000)
  const time = () =>
    new Intl.DateTimeFormat(presentationContext().locale, {
      timeStyle: "short",
      timeZone: presentationContext().timeZone,
    }).format(timestamp())
  const detailsID = () => `audit-record-${props.event.id}`
  return (
    <>
      <tr role="row" class={styles.row} data-failed={outcome().tone === "failure"}>
        <td role="cell" class={css({ gridColumn: "1 / 3", gridRow: "1" })}>
          <div
            class={css({
              display: "flex",
              alignItems: "baseline",
              flexWrap: "wrap",
              columnGap: "1.5",
              fontWeight: "medium",
              marginBottom: "1",
            })}
          >
            <span>{auditActionLabel(props.event.action)}</span>{" "}
            <Show
              when={
                target() &&
                (props.event.target_type === "user" || props.event.target_type === "library")
              }
            >
              <span aria-hidden="true" class={styles.muted}>
                ·
              </span>{" "}
              <bdi title={props.event.target_id}>{target()}</bdi>
            </Show>
          </div>
          <div
            class={css({
              display: "flex",
              alignItems: "baseline",
              flexWrap: "wrap",
              columnGap: "2",
              color: "fg.muted",
              fontSize: "xs",
            })}
          >
            <span>{targetType(props.event.target_type)}</span>{" "}
            <Show
              when={
                target() &&
                props.event.target_type !== "user" &&
                props.event.target_type !== "library"
              }
            >
              <code title={props.event.target_id}>{target()}</code>{" "}
            </Show>
            <span
              class={css({
                display: "inline-flex",
                alignItems: "center",
                gap: "1",
                "&[data-failed=true]": { color: "danger.default", fontWeight: "medium" },
              })}
              data-failed={outcome().tone === "failure"}
            >
              <Show when={outcome().tone === "success"}>
                <Check aria-hidden="true" size={13} />
              </Show>
              <Show when={outcome().tone === "failure"}>
                <CircleX aria-hidden="true" size={13} />
              </Show>
              {outcome().label}
            </span>
          </div>
          <Show when={props.event.library_id && props.event.target_type !== "library"}>
            <div class={styles.muted}>
              {m.account_audit_target_library()} ·{" "}
              <bdi>{library()?.name ?? compactAuditID(props.event.library_id!)}</bdi>
            </div>
          </Show>
        </td>
        <td role="cell" class={css({ gridColumn: "1", gridRow: "2", color: "fg.muted" })}>
          <bdi title={props.event.actor_id}>{auditActor(props.event.actor_id, props.names)}</bdi>
        </td>
        <td
          role="cell"
          class={css({
            gridColumn: "2",
            gridRow: "2",
            color: "fg.muted",
            fontSize: "xs",
            fontVariantNumeric: "tabular-nums",
          })}
        >
          <time
            dateTime={timestamp().toISOString()}
            title={formatEpoch(props.event.created_at)}
            aria-label={formatEpoch(props.event.created_at)}
          >
            {time()}
          </time>
        </td>
        <td
          role="cell"
          class={css({
            gridColumn: "3",
            gridRow: "1 / 3",
            alignSelf: "center",
            textAlign: "end",
            paddingInlineEnd: { md: "0!" },
          })}
        >
          <button
            type="button"
            aria-expanded={expanded()}
            aria-controls={expanded() ? detailsID() : undefined}
            aria-label={`${m.account_audit_details()}: ${auditActionLabel(props.event.action)}${target() ? ` · ${target()}` : ""} · ${formatEpoch(props.event.created_at)}`}
            onClick={() => setExpanded(!expanded())}
            class={css({
              display: "inline-flex",
              justifyContent: "center",
              alignItems: "center",
              minWidth: "8",
              minHeight: "8",
              borderRadius: "l1",
              cursor: "pointer",
              color: "fg.muted",
              _hover: { background: "bg.muted", color: "fg.default" },
              _focusVisible: {
                outline: "2px solid",
                outlineColor: "accent.default",
                outlineOffset: "2px",
              },
              "@media (pointer: coarse)": { minWidth: "11", minHeight: "11" },
            })}
          >
            <ChevronRight
              aria-hidden="true"
              size={16}
              data-expanded={expanded()}
              class={css({ "&[data-expanded=true]": { transform: "rotate(90deg)" } })}
            />
          </button>
        </td>
      </tr>
      <Show when={expanded()}>
        <tr role="row" class={css({ display: { base: "block", md: "table-row" } })}>
          <td role="cell" colSpan={4} id={detailsID()} class={styles.details}>
            <div
              class={css({
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: "3",
                marginBottom: "3",
              })}
            >
              <h3 class={css({ fontSize: "sm", fontWeight: "medium" })}>
                {m.account_audit_details()}
              </h3>
              <CopyButton
                size="xs"
                value={JSON.stringify(props.event, null, 2)}
                copyLabel={m.account_copy()}
                copiedLabel={m.account_copied()}
              />
            </div>
            <p class={css({ fontSize: "sm", color: "fg.muted", marginBottom: "4" })}>
              <time dateTime={timestamp().toISOString()}>
                {new Intl.DateTimeFormat(presentationContext().locale, {
                  dateStyle: "medium",
                  timeStyle: "long",
                  timeZone: presentationContext().timeZone,
                }).format(timestamp())}
              </time>
            </p>
            <dl
              class={css({
                display: "grid",
                gridTemplateColumns: { base: "1fr", md: "repeat(2, minmax(0, 1fr))" },
                columnGap: "6",
                fontSize: "sm",
              })}
            >
              <For
                each={[
                  [m.account_audit_event_id(), String(props.event.id)],
                  [m.account_audit_action_code(), props.event.action],
                  [m.account_audit_actor(), props.event.actor_id],
                  [m.account_audit_target(), props.event.target_id],
                  [m.account_audit_affected(), props.event.target_type],
                  [m.account_audit_result(), props.event.outcome],
                  [m.account_audit_target_library(), props.event.library_id],
                  [m.account_audit_credential_id(), props.event.credential_id],
                  [m.account_audit_request_id(), props.event.correlation_id],
                ].filter(([, value]) => Boolean(value))}
              >
                {(field) => (
                  <div
                    class={css({
                      display: "grid",
                      gridTemplateColumns: {
                        base: "minmax(0, 7rem) minmax(0, 1fr)",
                        md: "minmax(0, 1fr)",
                      },
                      gap: { base: "3", md: "1" },
                      alignContent: "start",
                      paddingBlock: "2.5",
                      borderTopWidth: "1px",
                      borderColor: "border.subtle",
                      alignItems: "baseline",
                    })}
                  >
                    <dt class={css({ color: "fg.muted" })}>{field[0]}</dt>
                    <dd class={css({ minWidth: "0" })}>
                      <code
                        class={css({
                          overflowWrap: "anywhere",
                          fontSize: "xs",
                          lineHeight: "tall",
                        })}
                      >
                        {field[1]}
                      </code>
                    </dd>
                  </div>
                )}
              </For>
            </dl>
          </td>
        </tr>
      </Show>
    </>
  )
}

const targetTypes: Record<string, () => string> = {
  authentication: () => m.account_audit_target_authentication(),
  book: () => m.account_audit_target_book(),
  invitation: () => m.account_audit_target_invitation(),
  library: () => m.account_audit_target_library(),
  mapping: () => m.account_audit_target_mapping(),
  passkey: () => m.account_audit_target_passkey(),
  provider: () => m.account_audit_target_provider(),
  recipe: () => m.account_audit_target_recipe(),
  session: () => m.account_audit_target_session(),
  settings: () => m.account_audit_target_settings(),
  token: () => m.account_audit_target_token(),
  user: () => m.account_audit_target_user(),
  webhook: () => m.account_audit_webhook(),
}

function targetType(type: string) {
  return Object.hasOwn(targetTypes, type) ? targetTypes[type]!() : type
}

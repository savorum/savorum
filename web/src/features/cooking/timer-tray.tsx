import { Link } from "@tanstack/solid-router"
import { BellRing, Timer, X } from "lucide-solid"
import { For, Show, createMemo } from "solid-js"
import { css } from "styled-system/css"

import { Button } from "@/components/ui/button"
import { ToggleField } from "@/components/ui/field"
import { IconButton } from "@/components/ui/icon-button"
import { Popover } from "@/components/ui/popover"
import {
  dismissFinishedTimers,
  dismissTimer,
  enableTimerNotifications,
  notificationsAvailable,
  notificationsBlocked,
  setTimerAlerts,
  timerAlerts,
  timerNow,
  timers,
} from "@/features/cooking/timer-clock"
import { formatNumber } from "@/lib/locale-format"
import { stepAnchorID } from "@/lib/recipe-format"
import { type CookTimer, countdown, finishedAgo, remainingSeconds, timerOrigin } from "@/lib/timers"

import { m } from "../../lib/messages.ts"

const styles = {
  trigger: css({
    alignItems: "center",
    background: "bg.subtle",
    borderColor: "border.default",
    borderRadius: "l1",
    borderWidth: "1px",
    color: "fg.muted",
    cursor: "pointer",
    display: "inline-flex",
    fontSize: "xs",
    fontVariantNumeric: "tabular-nums",
    gap: "1.5",
    paddingBlock: "1",
    paddingInline: "2",
    _hover: { color: "fg.default" },
    _focusVisible: {
      outline: "2px solid",
      outlineColor: "accent.default",
      outlineOffset: "2px",
    },
    // accent.solid.fg is the colour that belongs on a solid accent fill. There
    // is no accent.fg token, and asking for one silently leaves the inherited
    // muted grey on a dark green chip.
    '&[data-done="true"]': {
      background: "accent.default",
      borderColor: "accent.default",
      color: "accent.solid.fg",
      _hover: { color: "accent.solid.fg" },
    },
  }),
  count: css({ color: "inherit", opacity: "0.7" }),
  panel: css({ display: "grid", gap: "2", width: "22rem", maxWidth: "100%" }),
  list: css({ display: "grid", gap: "2", marginTop: "1" }),
  entry: css({
    alignItems: "center",
    borderColor: "border.default",
    borderRadius: "l2",
    borderWidth: "1px",
    display: "grid",
    gap: "2",
    gridTemplateColumns: "minmax(0, 1fr) auto auto",
    padding: "2.5",
    '&[data-done="true"]': {
      background: "accent.subtle.bg",
      borderColor: "accent.default",
    },
  }),
  label: css({ fontSize: "sm", fontWeight: "medium" }),
  origin: css({
    color: "fg.muted",
    display: "block",
    fontSize: "xs",
    overflow: "hidden",
    textDecoration: "none",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    _hover: { color: "accent.default", textDecoration: "underline" },
  }),
  clock: css({
    fontSize: "md",
    fontVariantNumeric: "tabular-nums",
    whiteSpace: "nowrap",
  }),
  done: css({
    color: "accent.default",
    fontSize: "sm",
    fontWeight: "semibold",
    whiteSpace: "nowrap",
  }),
  actions: css({
    borderColor: "border.subtle",
    borderTopWidth: "1px",
    display: "flex",
    gap: "2",
    justifyContent: "flex-end",
    marginTop: "1",
    paddingTop: "3",
  }),
  settings: css({
    borderColor: "border.subtle",
    borderTopWidth: "1px",
    display: "grid",
    gap: "2",
    marginTop: "1",
    paddingTop: "3",
  }),
  note: css({
    color: "fg.muted",
    fontSize: "xs",
    lineHeight: "1.5",
    margin: "0",
  }),
}

/** The soonest to come due, so the header shows the one that matters next. */
function mostUrgent(list: CookTimer[]): CookTimer | undefined {
  const finished = list.filter((timer) => timer.status === "done")
  if (finished.length > 0)
    return finished.reduce((first, timer) =>
      (timer.finishedAt ?? 0) > (first.finishedAt ?? 0) ? timer : first,
    )
  return list.reduce<CookTimer | undefined>(
    (first, timer) => (!first || timer.endsAt < first.endsAt ? timer : first),
    undefined,
  )
}

/**
 * Running timers, reachable from the header on every page and shown only once
 * a timer exists. It opens as a non-modal panel so the recipe stays readable.
 */
export function TimerTray() {
  const ordered = createMemo(() =>
    [...timers()].sort((first, second) => first.endsAt - second.endsAt),
  )
  const finished = createMemo(() => ordered().filter((timer) => timer.status === "done"))
  const leader = createMemo(() => mostUrgent(ordered()))

  const summary = () => {
    const timer = leader()
    if (!timer) return ""
    return timer.status === "done"
      ? m.timer_summary_finished({ label: timer.label })
      : countdown(remainingSeconds(timer, timerNow()))
  }

  return (
    <>
      <Show when={ordered().length > 0}>
        <Popover
          title={m.timer_tray_timers()}
          positioning={{ placement: "bottom-end", gutter: 8 }}
          trigger={(triggerProps) => (
            <button
              {...triggerProps()}
              type="button"
              class={styles.trigger}
              data-done={finished().length > 0}
              aria-label={m.timer_tray_label({ summary: summary() })}
            >
              <Show when={finished().length > 0} fallback={<Timer aria-hidden="true" size={13} />}>
                <BellRing aria-hidden="true" size={13} />
              </Show>
              {summary()}
              <Show when={ordered().length > 1}>
                <span class={styles.count}>
                  {m.common_plus_symbol()}
                  {formatNumber(ordered().length - 1)}
                </span>
              </Show>
            </button>
          )}
        >
          <div class={styles.panel}>
            <div class={styles.list}>
              <For each={ordered()}>
                {(timer) => (
                  <div class={styles.entry} data-done={timer.status === "done"}>
                    <div>
                      <div class={styles.label}>{timer.label}</div>
                      <Show when={timerOrigin(timer)}>
                        {(origin) => (
                          // Links to the step itself, not the top of the recipe.
                          <Show
                            when={timer.recipeID}
                            fallback={<span class={styles.origin}>{origin()}</span>}
                          >
                            <Link
                              class={styles.origin}
                              to="/recipes/$recipeId"
                              params={{ recipeId: timer.recipeID! }}
                              hash={timer.stepID ? stepAnchorID(timer.stepID) : undefined}
                            >
                              {origin()}
                            </Link>
                          </Show>
                        )}
                      </Show>
                    </div>
                    <Show
                      when={timer.status === "done"}
                      fallback={
                        <span class={styles.clock}>
                          {countdown(remainingSeconds(timer, timerNow()))}
                        </span>
                      }
                    >
                      <span class={styles.done}>
                        {m.timer_finished_ago({ elapsed: finishedAgo(timer, timerNow()) })}
                      </span>
                    </Show>
                    <IconButton
                      aria-label={m.timer_dismiss_label({ label: timer.label })}
                      size="xs"
                      variant="plain"
                      onClick={() => dismissTimer(timer.id)}
                    >
                      <X aria-hidden="true" size={15} />
                    </IconButton>
                  </div>
                )}
              </For>
            </div>

            <Show when={finished().length > 0}>
              <div class={styles.actions}>
                <Button size="sm" variant="outline" onClick={dismissFinishedTimers}>
                  {m.timer_tray_clear_finished()}
                </Button>
              </div>
            </Show>

            <div class={styles.settings}>
              <ToggleField
                label={m.timer_tray_sound_when_a_timer_finishes()}
                checked={timerAlerts().sound}
                onChange={(sound) => setTimerAlerts({ ...timerAlerts(), sound })}
              />
              <Show when={notificationsAvailable()}>
                <ToggleField
                  label={m.timer_tray_system_notifications()}
                  checked={timerAlerts().notifications}
                  disabled={notificationsBlocked()}
                  onChange={(wanted) => {
                    if (!wanted) {
                      setTimerAlerts({ ...timerAlerts(), notifications: false })
                      return
                    }
                    // The browser's permission prompt is raised here, as the
                    // answer to this switch, and never on arriving at a recipe.
                    void enableTimerNotifications()
                  }}
                />
              </Show>
              <Show when={notificationsBlocked()}>
                <p class={styles.note}>
                  {" "}
                  {m.timer_tray_this_browser_is_blocking_notifications_from_savorum_all()}{" "}
                </p>
              </Show>
              <p class={styles.note}>
                {" "}
                {m.timer_tray_timers_are_kept_on_this_device_and_count_down_against_t()}{" "}
              </p>
            </div>
          </div>
        </Popover>
      </Show>
    </>
  )
}

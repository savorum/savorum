import { createSignal, type Accessor } from "solid-js"

export interface AsyncActionOptions {
  /** Run after the change finishes, whether it succeeded or was refused. */
  onSettled?: () => void | Promise<void>
}

export interface KeyedAction<K> {
  /** Which change is running, rather than merely that one is. */
  busy: Accessor<K | undefined>
  /**
   * Why the last change was refused, kept as thrown so `ChangeError` can show
   * an `ApiError`'s field-level causes.
   */
  error: Accessor<unknown>
  setError: (value: unknown) => void
  notice: Accessor<string>
  setNotice: (message: string) => void
  run: (key: K, action: () => Promise<unknown>, notice?: string) => Promise<void>
}

/**
 * One change in flight, for a screen whose rows each have their own control.
 *
 * The key names the change, so a list of thirty rows disables the one button
 * that was pressed rather than all of them.
 */
export function createKeyedAction<K>(options: AsyncActionOptions = {}): KeyedAction<K> {
  const [busy, setBusy] = createSignal<K | undefined>()
  const [error, setError] = createSignal<unknown>()
  const [notice, setNotice] = createSignal("")

  const run = async (key: K, action: () => Promise<unknown>, message = "") => {
    setBusy(() => key)
    setError(undefined)
    setNotice("")
    try {
      await action()
      setNotice(message)
    } catch (failure) {
      setError(failure)
    } finally {
      setBusy(undefined)
      await options.onSettled?.()
    }
  }

  return { busy, error, setError: (value) => setError(() => value), notice, setNotice, run }
}

export interface AsyncAction extends Omit<KeyedAction<true>, "busy" | "run"> {
  busy: Accessor<boolean>
  run: (action: () => Promise<unknown>, notice?: string) => Promise<void>
}

/** One change in flight, for a screen that runs them one at a time. */
export function createAsyncAction(options: AsyncActionOptions = {}): AsyncAction {
  const keyed = createKeyedAction<true>(options)
  return {
    ...keyed,
    busy: () => keyed.busy() === true,
    run: (action, notice) => keyed.run(true, action, notice),
  }
}

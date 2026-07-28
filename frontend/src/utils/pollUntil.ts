export type PollUntilOptions<T> = {
  timeoutMs: number
  intervalMs: number
  poll: () => Promise<T> | T
  done: (value: T) => boolean
}

export function sleep(milliseconds: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds))
}

export async function pollUntil<T>(options: PollUntilOptions<T>) {
  const deadline = Date.now() + options.timeoutMs
  for (;;) {
    await sleep(options.intervalMs)
    const value = await options.poll()
    if (options.done(value) || Date.now() >= deadline) return value
  }
}

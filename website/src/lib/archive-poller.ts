/**
 * Polls one archive request until it finishes: every 2 s at first, then up to
 * every 15 s. One request at a time; none while the page is hidden (it resumes
 * once shown); none after an answer that ends the wait. Kept apart from React
 * so its timing rules can be tested.
 */

export const FIRST_POLL_MS = 2_000;
export const LONGEST_POLL_MS = 15_000;

export type PollerOptions<Outcome> = {
  /** Reads the status once; must not throw. */
  read: (signal: AbortSignal) => Promise<Outcome>;
  /** Whether to ask again after this outcome. */
  keepPolling: (outcome: Outcome) => boolean;
  onOutcome: (outcome: Outcome) => void;
  isHidden: () => boolean;
  setTimer: (callback: () => void, ms: number) => unknown;
  clearTimer: (timer: unknown) => void;
};

export type Poller = {
  start: () => void;
  /** Call when the page becomes visible. */
  visibilityChanged: () => void;
  stop: () => void;
};

export function createArchivePoller<Outcome>(options: PollerOptions<Outcome>): Poller {
  let stopped = false;
  let finished = false;
  let inFlight = false;
  let waitingForVisible = false;
  let timer: unknown = null;
  let delay = FIRST_POLL_MS;
  let request: AbortController | null = null;

  const schedule = () => {
    timer = options.setTimer(() => {
      timer = null;
      void poll();
    }, delay);
    delay = Math.min(Math.round(delay * 1.5), LONGEST_POLL_MS);
  };

  async function poll() {
    if (stopped || finished || inFlight) {
      return;
    }
    if (options.isHidden()) {
      waitingForVisible = true;
      return;
    }
    inFlight = true;
    request = new AbortController();
    const outcome = await options.read(request.signal);
    inFlight = false;
    request = null;
    if (stopped) {
      return;
    }
    options.onOutcome(outcome);
    if (options.keepPolling(outcome)) {
      schedule();
    } else {
      finished = true;
    }
  }

  return {
    start: () => void poll(),
    visibilityChanged: () => {
      if (!stopped && waitingForVisible && !options.isHidden()) {
        waitingForVisible = false;
        void poll();
      }
    },
    stop: () => {
      stopped = true;
      request?.abort();
      if (timer !== null) {
        options.clearTimer(timer);
        timer = null;
      }
    },
  };
}

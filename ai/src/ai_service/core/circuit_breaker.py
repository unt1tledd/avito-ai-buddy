from time import monotonic


class CircuitBreaker:
    """One circuit per upstream/process, with a single recovery probe.

    State transitions don't await, so concurrent calls on the application's
    event loop cannot acquire two probes. Generations ignore stale completions.
    """

    def __init__(self, threshold: int, reset_seconds: float, *, clock=monotonic):
        self.clock = clock
        self.threshold = threshold
        self.reset_seconds = reset_seconds
        self.failures = 0
        self.open_until: float | None = None
        self.probing = False
        self.generation = 0

    def acquire(self) -> int | None:
        if self.open_until is not None:
            if self.clock() < self.open_until or self.probing:
                return None
            self.probing = True
        return self.generation

    def succeeded(self, generation: int) -> None:
        if generation != self.generation:
            return
        self.failures = 0
        if self.open_until is not None:
            self.open_until = None
            self.probing = False
            self.generation += 1

    def failed(self, generation: int) -> None:
        if generation != self.generation:
            return
        self.failures += 1
        if self.probing or self.failures >= self.threshold:
            self.open_until = self.clock() + self.reset_seconds
            self.probing = False
            self.generation += 1

    def cancelled(self, generation: int) -> None:
        if generation == self.generation:
            self.probing = False


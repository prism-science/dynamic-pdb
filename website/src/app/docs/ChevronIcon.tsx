const ROTATION = { down: 0, left: 90, up: 180, right: 270 } as const;

export default function ChevronIcon({
  direction = "down",
  size = 12,
}: {
  direction?: keyof typeof ROTATION;
  size?: number;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      style={{ transform: `rotate(${ROTATION[direction]}deg)` }}
    >
      <path d="m6 9 6 6 6-6" />
    </svg>
  );
}

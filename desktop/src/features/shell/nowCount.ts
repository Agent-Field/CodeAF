/** The Now count in ink-3 caption type; unknown or zero is nothing, never "0" (the emptiness law). */
export const nowCount = (count?: number) => (count && count > 0 ? count : undefined);

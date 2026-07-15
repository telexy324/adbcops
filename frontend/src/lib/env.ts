import { z } from "zod";

const environmentSchema = z.object({
  VITE_API_BASE_URL: z
    .string()
    .trim()
    .default("")
    .transform((value) => {
      const normalized = value.replace(/\/+$/, "");
      if (normalized === "/api") {
        return "";
      }
      return normalized.endsWith("/api")
        ? normalized.slice(0, -"/api".length)
        : normalized;
    }),
  VITE_API_TIMEOUT_MS: z.coerce.number().int().positive().default(180_000),
});

export const environment = environmentSchema.parse({
  VITE_API_BASE_URL: import.meta.env.VITE_API_BASE_URL,
  VITE_API_TIMEOUT_MS: import.meta.env.VITE_API_TIMEOUT_MS,
});

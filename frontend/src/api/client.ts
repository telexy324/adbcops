import axios from "axios";

import { environment } from "@/lib/env";

export const apiClient = axios.create({
  baseURL: environment.VITE_API_BASE_URL,
  timeout: 15_000,
  headers: {
    Accept: "application/json",
  },
});

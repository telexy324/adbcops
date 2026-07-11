import axios from "axios";

import { environment } from "@/lib/env";

export const apiClient = axios.create({
  baseURL: environment.VITE_API_BASE_URL,
  timeout: 15_000,
  headers: {
    Accept: "application/json",
  },
});

const tokenStorageKey = "adbcops.accessToken";

export function getAccessToken() {
  return window.localStorage.getItem(tokenStorageKey);
}

export function setAccessToken(token: string) {
  window.localStorage.setItem(tokenStorageKey, token);
}

export function clearAccessToken() {
  window.localStorage.removeItem(tokenStorageKey);
}

apiClient.interceptors.request.use((config) => {
  const token = getAccessToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

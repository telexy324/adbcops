import axios from "axios";

import { environment } from "@/lib/env";

export const apiClient = axios.create({
  baseURL: environment.VITE_API_BASE_URL,
  timeout: environment.VITE_API_TIMEOUT_MS,
  headers: {
    Accept: "application/json",
  },
});

const tokenStorageKey = "adbcops.accessToken";
const userStorageKey = "adbcops.currentUser";

export type SessionUser = {
  id: number;
  username: string;
  displayName?: string;
  role: string;
};

export function getAccessToken() {
  return window.localStorage.getItem(tokenStorageKey);
}

export function setAccessToken(token: string) {
  window.localStorage.setItem(tokenStorageKey, token);
}

export function clearAccessToken() {
  window.localStorage.removeItem(tokenStorageKey);
  window.localStorage.removeItem(userStorageKey);
}

export function setCurrentUser(user: SessionUser) {
  window.localStorage.setItem(userStorageKey, JSON.stringify(user));
}

export function getCurrentUser(): SessionUser | null {
  try {
    const value = window.localStorage.getItem(userStorageKey);
    return value ? (JSON.parse(value) as SessionUser) : null;
  } catch {
    return null;
  }
}

apiClient.interceptors.request.use((config) => {
  const token = getAccessToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

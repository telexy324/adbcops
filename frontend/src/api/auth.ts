import { apiClient, setAccessToken } from "@/api/client";

type ApiEnvelope<T> = {
  code: number;
  message: string;
  data: T;
};

export type LoginResponse = {
  accessToken: string;
  tokenType: string;
  expiresAt: string;
  user: {
    id: number;
    username: string;
    displayName?: string;
    role: string;
  };
};

export async function login(input: { username: string; password: string }) {
  const response = await apiClient.post<ApiEnvelope<LoginResponse>>(
    "/api/auth/login",
    input,
  );
  setAccessToken(response.data.data.accessToken);
  return response.data.data;
}

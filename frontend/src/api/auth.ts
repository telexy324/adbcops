import { apiClient, setAccessToken, setCurrentUser } from "@/api/client";

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
  setCurrentUser(response.data.data.user);
  return response.data.data;
}

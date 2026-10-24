// SPDX-License-Identifier: Apache-2.0
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode
} from "react";
import {
  APIError,
  errorMessage,
  fetchMe,
  login as loginRequest,
  logout as logoutRequest,
  register as registerRequest,
  type PersonalAI,
  type User
} from "./api";

type AuthStatus = "loading" | "anonymous" | "authenticated" | "error";

interface AuthValue {
  status: AuthStatus;
  user: User | null;
  profile: PersonalAI | null;
  login: (email: string, password: string) => Promise<void>;
  register: (input: {
    email: string;
    handle: string;
    display_name: string;
    password: string;
  }) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
  logoutError: string;
  clearLogoutError: () => void;
  authError: unknown;
}

const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [user, setUser] = useState<User | null>(null);
  const [profile, setProfile] = useState<PersonalAI | null>(null);
  const [logoutError, setLogoutError] = useState("");
  const [authError, setAuthError] = useState<unknown>(null);

  const refresh = useCallback(async () => {
    setStatus("loading");
    setAuthError(null);
    try {
      const response = await fetchMe();
      setUser(response.user);
      setProfile(response.personal_ai);
      setStatus("authenticated");
    } catch (error) {
      if (error instanceof APIError && error.status === 401) {
        setUser(null);
        setProfile(null);
        setAuthError(null);
        setStatus("anonymous");
        return;
      }
      // A network or server failure is not a sign-out. Keep the failure
      // visible and let the user retry without losing the distinction.
      setAuthError(error);
      setStatus("error");
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    const onUnauthorized = () => {
      setUser(null);
      setProfile(null);
      setStatus("anonymous");
    };
    window.addEventListener("dw:unauthorized", onUnauthorized);
    return () => window.removeEventListener("dw:unauthorized", onUnauthorized);
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const response = await loginRequest(email, password);
    setUser(response.user);
    setProfile(response.personal_ai);
    setLogoutError("");
    setAuthError(null);
    setStatus("authenticated");
  }, []);

  const register = useCallback(
    async (input: { email: string; handle: string; display_name: string; password: string }) => {
      const response = await registerRequest(input);
      setUser(response.user);
      setProfile(response.personal_ai);
      setLogoutError("");
      setAuthError(null);
      setStatus("authenticated");
    },
    []
  );

  const logout = useCallback(async () => {
    setLogoutError("");
    try {
      await logoutRequest();
    } catch (failure) {
      if (failure instanceof APIError && failure.status === 401) {
        // The server already considers the session invalid.
      } else {
        // Keep the authenticated state: a failed revocation must not look like
        // a successful sign-out on a shared machine.
        setLogoutError(errorMessage(failure));
        return;
      }
    }
    setUser(null);
    setProfile(null);
    setStatus("anonymous");
  }, []);

  const clearLogoutError = useCallback(() => setLogoutError(""), []);

  const value = useMemo(
    () => ({
      status,
      user,
      profile,
      login,
      register,
      logout,
      refresh,
      logoutError,
      clearLogoutError,
      authError
    }),
    [status, user, profile, login, register, logout, refresh, logoutError, clearLogoutError, authError]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return value;
}

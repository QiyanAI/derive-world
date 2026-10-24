// SPDX-License-Identifier: Apache-2.0
import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode
} from "react";
import { fetchMeta, type MetaResponse } from "./api";

export type ModelMode = "fixture" | "real" | "unknown";

interface MetaState {
  status: "loading" | "ready" | "error";
  mode: ModelMode;
  meta: MetaResponse | null;
}

const initialState: MetaState = {
  status: "loading",
  mode: "unknown",
  meta: null
};

const MetaContext = createContext<MetaState>(initialState);

export function MetaProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<MetaState>(initialState);

  useEffect(() => {
    const controller = new AbortController();
    fetchMeta(controller.signal)
      .then((meta) => {
        setState({ status: "ready", mode: meta.model_mode, meta });
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setState({ status: "error", mode: "unknown", meta: null });
        }
      });
    return () => controller.abort();
  }, []);

  return <MetaContext.Provider value={state}>{children}</MetaContext.Provider>;
}

export function useMeta(): MetaState {
  return useContext(MetaContext);
}

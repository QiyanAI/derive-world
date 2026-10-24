// SPDX-License-Identifier: Apache-2.0
import { Component, type ErrorInfo, type ReactNode } from "react";
import { useI18n } from "../i18n";

function ErrorFallback() {
  const { t } = useI18n();
  return (
    <div className="error-state" role="alert">
      <h1>{t("error.title")}</h1>
      <p>{t("error.body")}</p>
      <button
        type="button"
        className="button"
        onClick={() => window.location.reload()}
      >
        {t("error.reload")}
      </button>
    </div>
  );
}

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

export default class ErrorBoundary extends Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  override state: ErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("ui.error_boundary", {
      message: error.message,
      componentStack: info.componentStack
    });
  }

  override render(): ReactNode {
    if (this.state.hasError) {
      return <ErrorFallback />;
    }
    return this.props.children;
  }
}

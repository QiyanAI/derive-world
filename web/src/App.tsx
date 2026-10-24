// SPDX-License-Identifier: Apache-2.0
import { BrowserRouter, Route, Routes } from "react-router";
import AppShell from "./components/AppShell";
import ErrorBoundary from "./components/ErrorBoundary";
import { AuthProvider } from "./auth";
import { I18nProvider } from "./i18n";
import { MetaProvider } from "./meta";
import AIPage from "./pages/AIPage";
import DerivePage from "./pages/DerivePage";
import HomePage from "./pages/HomePage";
import InvitePage from "./pages/InvitePage";
import LineagePage from "./pages/LineagePage";
import LoginPage from "./pages/LoginPage";
import NotFoundPage from "./pages/NotFoundPage";
import ObjectNewPage from "./pages/ObjectNewPage";
import ObjectPage from "./pages/ObjectPage";
import ObjectsPage from "./pages/ObjectsPage";
import RegisterPage from "./pages/RegisterPage";
import SelfPage from "./pages/SelfPage";
import SettingsPage from "./pages/SettingsPage";
import WelcomePage from "./pages/WelcomePage";

export default function App() {
  return (
    <I18nProvider>
      <ErrorBoundary>
        <MetaProvider>
          <AuthProvider>
            <BrowserRouter>
              <Routes>
                <Route element={<AppShell />}>
                  <Route index element={<HomePage />} />
                  <Route path="welcome" element={<WelcomePage />} />
                  <Route path="login" element={<LoginPage />} />
                  <Route path="register" element={<RegisterPage />} />
                  <Route path="self" element={<SelfPage />} />
                  <Route path="ai" element={<AIPage />} />
                  <Route path="objects" element={<ObjectsPage />} />
                  <Route path="objects/new" element={<ObjectNewPage />} />
                  <Route path="objects/:id" element={<ObjectPage />} />
                  <Route path="objects/:id/derive" element={<DerivePage />} />
                  <Route path="objects/:id/lineage" element={<LineagePage />} />
                  <Route path="invite/:token" element={<InvitePage />} />
                  <Route path="settings" element={<SettingsPage />} />
                  <Route path="*" element={<NotFoundPage />} />
                </Route>
              </Routes>
            </BrowserRouter>
          </AuthProvider>
        </MetaProvider>
      </ErrorBoundary>
    </I18nProvider>
  );
}

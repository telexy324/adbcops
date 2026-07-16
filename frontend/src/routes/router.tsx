import { Navigate, createBrowserRouter } from "react-router-dom";

import { AppShell } from "@/components/layout/app-shell";
import { AnalysisPage } from "@/pages/analysis-page";
import { DashboardPage } from "@/pages/dashboard-page";
import { KnowledgePage } from "@/pages/knowledge-page";
import { LoginPage } from "@/pages/login-page";
import { NotFoundPage } from "@/pages/not-found-page";
import { OperationsPage } from "@/pages/operations-page";
import { QualityReviewPage } from "@/pages/quality-review-page";
import { SettingsPage } from "@/pages/settings-page";
import { TopologyConfigurationPage } from "@/pages/topology-configuration-page";
import { TopologyPage } from "@/pages/topology-page";
import { WorkflowPage } from "@/pages/workflow-page";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Navigate to="/login" replace />,
  },
  {
    path: "/login",
    element: <LoginPage />,
  },
  {
    element: <AppShell />,
    children: [
      { path: "/dashboard", element: <DashboardPage /> },
      { path: "/analysis", element: <AnalysisPage /> },
      { path: "/knowledge", element: <KnowledgePage /> },
      {
        path: "/knowledge/evaluations/:id/review",
        element: <QualityReviewPage />,
      },
      { path: "/workflows", element: <WorkflowPage /> },
      { path: "/topology", element: <TopologyPage /> },
      {
        path: "/topology/configuration",
        element: <TopologyConfigurationPage />,
      },
      { path: "/operations", element: <OperationsPage /> },
      { path: "/settings", element: <SettingsPage /> },
    ],
  },
  {
    path: "*",
    element: <NotFoundPage />,
  },
]);

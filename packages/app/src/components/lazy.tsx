import { type ComponentProps, lazy, Suspense } from "react";

// Screens most sessions never open load as their own chunks: Settings,
// and the vault wizard (first run on the desktop, Switch/New vault…).
// Both are fetched once the app is idle, so opening them does not wait.

const loadSettings = () => import("./Settings.tsx");
const loadOnboarding = () => import("../onboarding/Onboarding.tsx");

const SettingsPage = lazy(() => loadSettings().then((m) => ({ default: m.Settings })));
const OnboardingFlow = lazy(() => loadOnboarding().then((m) => ({ default: m.Onboarding })));

const blank = <div className="h-full bg-background" />;

export function Settings() {
  return (
    <Suspense fallback={blank}>
      <SettingsPage />
    </Suspense>
  );
}

export function Onboarding(props: ComponentProps<typeof OnboardingFlow>) {
  return (
    <Suspense fallback={blank}>
      <OnboardingFlow {...props} />
    </Suspense>
  );
}

/** Fetches the lazy screens in the background once the page is idle. */
export function preloadScreens() {
  const go = () => void Promise.all([loadSettings(), loadOnboarding()]).catch(() => {});
  if (typeof requestIdleCallback === "function") requestIdleCallback(go, { timeout: 3000 });
  else setTimeout(go, 1500);
}

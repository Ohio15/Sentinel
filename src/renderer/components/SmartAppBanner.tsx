/**
 * Smart App Banner - Prompts mobile users to download the native app
 * Only shows on mobile browsers, remembers dismissal
 */
import { useState, useEffect } from 'react';
import { X, Smartphone, ExternalLink } from 'lucide-react';

const STORAGE_KEY = 'sentinel-app-banner-dismissed';
const DISMISS_DURATION_DAYS = 7; // Show again after 7 days

interface SmartAppBannerProps {
  iosAppId?: string;
  androidPackage?: string;
  expoProjectUrl?: string;
}

export function SmartAppBanner({
  iosAppId,
  androidPackage,
  expoProjectUrl = 'https://expo.dev/@sentinel/sentinel-mobile',
}: SmartAppBannerProps) {
  const [isVisible, setIsVisible] = useState(false);
  const [platform, setPlatform] = useState<'ios' | 'android' | null>(null);

  useEffect(() => {
    // Check if we should show the banner
    const shouldShow = checkShouldShow();
    if (shouldShow) {
      setIsVisible(true);
      setPlatform(detectPlatform());
    }
  }, []);

  const checkShouldShow = (): boolean => {
    // Only show on mobile devices
    if (!isMobileDevice()) {
      return false;
    }

    // Check if user dismissed recently
    const dismissedAt = localStorage.getItem(STORAGE_KEY);
    if (dismissedAt) {
      const dismissDate = new Date(parseInt(dismissedAt, 10));
      const daysSinceDismiss = (Date.now() - dismissDate.getTime()) / (1000 * 60 * 60 * 24);
      if (daysSinceDismiss < DISMISS_DURATION_DAYS) {
        return false;
      }
    }

    return true;
  };

  const isMobileDevice = (): boolean => {
    const userAgent = navigator.userAgent || navigator.vendor || (window as Window & { opera?: string }).opera || '';

    // Check for mobile user agents
    const mobileRegex = /android|webos|iphone|ipad|ipod|blackberry|iemobile|opera mini/i;
    if (mobileRegex.test(userAgent.toLowerCase())) {
      return true;
    }

    // Check for touch capability + small screen (tablet/phone)
    if ('ontouchstart' in window && window.innerWidth < 1024) {
      return true;
    }

    return false;
  };

  const detectPlatform = (): 'ios' | 'android' | null => {
    const userAgent = navigator.userAgent.toLowerCase();

    if (/iphone|ipad|ipod/.test(userAgent)) {
      return 'ios';
    }
    if (/android/.test(userAgent)) {
      return 'android';
    }
    return null;
  };

  const handleDismiss = () => {
    localStorage.setItem(STORAGE_KEY, Date.now().toString());
    setIsVisible(false);
  };

  const getStoreUrl = (): string => {
    if (platform === 'ios' && iosAppId) {
      return `https://apps.apple.com/app/id${iosAppId}`;
    }
    if (platform === 'android' && androidPackage) {
      return `https://play.google.com/store/apps/details?id=${androidPackage}`;
    }
    // Fallback to Expo Go instructions
    return expoProjectUrl;
  };

  const getStoreName = (): string => {
    if (platform === 'ios' && iosAppId) {
      return 'App Store';
    }
    if (platform === 'android' && androidPackage) {
      return 'Google Play';
    }
    return 'Expo Go';
  };

  const handleOpenApp = () => {
    const url = getStoreUrl();
    window.open(url, '_blank', 'noopener,noreferrer');
  };

  if (!isVisible) {
    return null;
  }

  return (
    <div className="fixed top-0 left-0 right-0 z-50 bg-surface border-b border-border shadow-lg animate-slide-down">
      <div className="flex items-center gap-3 px-4 py-3">
        {/* App Icon */}
        <div className="flex-shrink-0 w-12 h-12 bg-primary/20 rounded-xl flex items-center justify-center">
          <Smartphone className="w-6 h-6 text-primary" />
        </div>

        {/* Text Content */}
        <div className="flex-1 min-w-0">
          <h3 className="text-sm font-semibold text-text-primary truncate">
            Sentinel Mobile
          </h3>
          <p className="text-xs text-text-secondary">
            Get the app for a better experience
          </p>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-2 flex-shrink-0">
          <button
            onClick={handleOpenApp}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-primary text-white text-sm font-medium rounded-lg hover:bg-primary-hover transition-colors"
          >
            <span>Open</span>
            <ExternalLink className="w-3.5 h-3.5" />
          </button>
          <button
            onClick={handleDismiss}
            className="p-1.5 text-text-secondary hover:text-text-primary hover:bg-surface-hover rounded-lg transition-colors"
            aria-label="Dismiss"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
      </div>

      {/* Store indicator */}
      <div className="px-4 pb-2">
        <span className="text-xs text-text-muted">
          {getStoreName() === 'Expo Go' ? (
            <>Install <strong>Expo Go</strong> app, then scan QR code</>
          ) : (
            <>Available on {getStoreName()}</>
          )}
        </span>
      </div>
    </div>
  );
}

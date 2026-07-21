import { useMutation } from "@tanstack/react-query";
import { BellRing, Check, Clock, Server, ShieldAlert, UserRound, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { ConsoleData, ManualReview } from "../types";

type ReviewState = {
  review: ManualReview;
  status: "pending" | "allowed" | "denied";
  submitting?: "allow" | "deny";
  error?: string;
};
type ReviewNotification = {
  notification: Notification;
  expiresAt: number;
};

const POLL_BACKOFF_MS = 500;
const POLL_MAX_BACKOFF_MS = 5000;

export function ManualReviewPoller({ data, sessionID = "" }: { data: ConsoleData; sessionID?: string }) {
  const { t } = useI18n();
  const [reviews, setReviews] = useState<ReviewState[]>([]);
  const [dismissing, setDismissing] = useState<Set<string>>(new Set());
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [notificationPermission, setNotificationPermission] = useState<NotificationPermission | "unsupported">(() => getNotificationPermission());
  const [notificationPromptHidden, setNotificationPromptHidden] = useState(false);
  const knownIDsRef = useRef<Set<string>>(new Set());
  const notifiedIDsRef = useRef<Set<string>>(new Set());
  const notificationsRef = useRef<Map<string, ReviewNotification>>(new Map());
  const canReview = useMemo(() => canReviewInOrg(data), [data]);

  useEffect(() => {
    closeReviewNotifications(notificationsRef.current);
    setReviews([]);
    setDismissing(new Set());
    setHidden(new Set());
    setNotificationPromptHidden(false);
    knownIDsRef.current = new Set();
    notifiedIDsRef.current = new Set();
  }, [canReview, data.activeOrg.id, sessionID]);

  useEffect(() => {
    return () => closeReviewNotifications(notificationsRef.current);
  }, []);

  useEffect(() => {
    const pruneExpired = () => {
      closeExpiredReviewNotifications(notificationsRef.current);
      setReviews((prev) => prev.filter((item) => item.status !== "pending" || isReviewActive(item.review)));
    };
    pruneExpired();
    const timer = window.setInterval(pruneExpired, 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    setNotificationPermission(getNotificationPermission());
  }, []);

  useEffect(() => {
    if (!canReview) return;
    let backoff = POLL_BACKOFF_MS;
    let cancelled = false;

    const run = async () => {
      while (!cancelled) {
        try {
          const result = await api.manualReviews(data.activeOrg.id, 25, Array.from(knownIDsRef.current), sessionID);
          if (cancelled) return;
          for (const review of result.reviews) {
            knownIDsRef.current.add(review.id);
          }
          const activeReviews = result.reviews.filter(isReviewActive);
          notifyPendingReviews(activeReviews, notifiedIDsRef.current, notificationsRef.current, t);
          setReviews((prev) => mergeReviews(prev, activeReviews));
          backoff = POLL_BACKOFF_MS;
          if (result.reviews.length === 0) await sleep(250);
        } catch {
          if (cancelled) return;
          await sleep(Math.min(backoff, POLL_MAX_BACKOFF_MS));
          backoff = Math.min(backoff * 2, POLL_MAX_BACKOFF_MS);
        }
      }
    };

    const timer = window.setTimeout(run, 0);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [canReview, data.activeOrg.id, sessionID]);

  const decide = useMutation({
    mutationFn: ({ id, allow, autoAllowMinutes }: { id: string; allow: boolean; autoAllowMinutes?: number }) =>
      api.decideManualReview(id, allow, autoAllowMinutes),
    onMutate: (variables) => {
      setReviews((prev) =>
        prev.map((item) =>
          item.review.id === variables.id
            ? { ...item, submitting: variables.allow ? "allow" : "deny", error: undefined }
            : item
        )
      );
    },
    onSuccess: (result, variables) => {
      closeReviewNotification(notificationsRef.current, variables.id);
      setReviews((prev) =>
        prev.map((item) => {
          return item.review.id === variables.id
            ? {
                ...item,
                review: {
                  ...item.review,
                  auto_allow_minutes: result.auto_allow_minutes,
                  auto_allow_expires_at: result.auto_allow_expires_at,
                },
                status: variables.allow ? "allowed" : "denied",
                submitting: undefined,
                error: undefined,
              }
            : item;
        })
      );
      setDismissing((prev) => new Set(prev).add(variables.id));
      window.setTimeout(() => {
        setHidden((prev) => new Set(prev).add(variables.id));
      }, 1200);
    },
    onError: (error, variables) => {
      setReviews((prev) =>
        prev.map((item) =>
          item.review.id === variables.id
            ? { ...item, submitting: undefined, error: error instanceof Error ? error.message : t("manualReviewSubmitFailed") }
            : item
        )
      );
    },
  });

  const visible = reviews.filter((item) => !hidden.has(item.review.id));
  const showNotificationPrompt = canReview && notificationPermission === "default" && !notificationPromptHidden;

  if (!visible.length && !showNotificationPrompt) return null;

  return createPortal(
    <div className="manual-review-toasts" aria-live="polite" aria-atomic="false">
      {showNotificationPrompt && (
        <NotificationPermissionPrompt
          t={t}
          onEnable={async () => {
            const permission = await requestNotificationPermission();
            setNotificationPermission(permission);
          }}
          onDismiss={() => setNotificationPromptHidden(true)}
        />
      )}
      {visible.map((item) => (
        <ReviewCard
          key={item.review.id}
          item={item}
          dismissing={dismissing.has(item.review.id)}
          onAllow={(autoAllowMinutes) => {
            if (item.status === "pending" && !item.submitting) decide.mutate({ id: item.review.id, allow: true, autoAllowMinutes });
          }}
          onDeny={(autoAllowMinutes) => {
            if (item.status === "pending" && !item.submitting) decide.mutate({ id: item.review.id, allow: false, autoAllowMinutes });
          }}
          onExpire={() => {
            setReviews((prev) => prev.filter((review) => review.review.id !== item.review.id));
            closeReviewNotification(notificationsRef.current, item.review.id);
          }}
          onDismiss={() => {
            closeReviewNotification(notificationsRef.current, item.review.id);
            setHidden((prev) => new Set(prev).add(item.review.id));
          }}
          compact={Boolean(data.runtime.client_mode)}
          t={t}
        />
      ))}
    </div>,
    document.body
  );
}

function NotificationPermissionPrompt({
  onEnable,
  onDismiss,
  t,
}: {
  onEnable: () => Promise<void>;
  onDismiss: () => void;
  t: (key: string, fallback?: string) => string;
}) {
  return (
    <div className="manual-review-notification-prompt">
      <BellRing />
      <div>
        <strong>{t("manualReviewNotificationTitle")}</strong>
        <p>{t("manualReviewNotificationBody")}</p>
      </div>
      <button type="button" className="primary" onClick={() => void onEnable()}>
        {t("manualReviewNotificationEnable")}
      </button>
      <button type="button" className="manual-review-close" onClick={onDismiss} aria-label={t("close")}>
        <X />
      </button>
    </div>
  );
}

function ReviewCard({
  item,
  dismissing,
  onAllow,
  onDeny,
  onExpire,
  onDismiss,
  compact,
  t,
}: {
  item: ReviewState;
  dismissing: boolean;
  onAllow: (autoAllowMinutes?: number) => void;
  onDeny: (autoAllowMinutes?: number) => void;
  onExpire: () => void;
  onDismiss: () => void;
  compact?: boolean;
  t: (key: string, fallback?: string) => string;
}) {
  const { review, status } = item;
  const isSubmitting = Boolean(item.submitting);
  const activeAutoAllow = Boolean(review.auto_allow_minutes && review.auto_allow_expires_at && secondsUntil(review.auto_allow_expires_at) > 0);
  const configuredMinutes = review.auto_allow_minutes || 10;
  const countdownAt = review.expires_at;
  const [autoAllowEnabled, setAutoAllowEnabled] = useState(activeAutoAllow);
  const [autoAllowMinutes, setAutoAllowMinutes] = useState(configuredMinutes);
  const [secondsLeft, setSecondsLeft] = useState(() => secondsUntil(countdownAt));
  const intervalRef = useRef<number | null>(null);

  useEffect(() => {
    const update = () => setSecondsLeft(secondsUntil(countdownAt));
    update();
    intervalRef.current = window.setInterval(() => {
      update();
    }, 1000);
    return () => {
      if (intervalRef.current) window.clearInterval(intervalRef.current);
    };
  }, [countdownAt]);

  useEffect(() => {
    setAutoAllowEnabled(activeAutoAllow);
    setAutoAllowMinutes(configuredMinutes);
  }, [activeAutoAllow, configuredMinutes, review.auto_allow_expires_at]);

  useEffect(() => {
    if (secondsLeft === 0 && status === "pending") {
      onExpire();
    }
  }, [secondsLeft, status, onExpire]);

  const isDone = status === "allowed" || status === "denied";
  const validAutoAllowMinutes = Number.isInteger(autoAllowMinutes) && autoAllowMinutes >= 1 && autoAllowMinutes <= 1440;
  const rememberedMinutes = autoAllowEnabled ? autoAllowMinutes : (activeAutoAllow ? 0 : undefined);
  return (
    <div className={`manual-review-card ${isDone ? "done" : ""} ${dismissing ? "dismissing" : ""}`}>
      <div className="manual-review-header">
        <ShieldAlert />
        <strong>{t("manualReviewTitle")}</strong>
        <span className="manual-review-countdown">
          <Clock />
          {t(review.default_allow ? "manualReviewDefaultAllowCountdown" : "manualReviewDefaultDenyCountdown")
            .replace("{seconds}", String(secondsLeft))}
        </span>
        <button type="button" className="manual-review-close" onClick={onDismiss} aria-label={t("close")}>
          <X />
        </button>
      </div>
      <div className="manual-review-body">
        <div className="manual-review-row">
          <span className="manual-review-label"><Server />{t("manualReviewTarget")}</span>
          <strong>{review.target_name}</strong>
          <code>{review.target_alias}</code>
        </div>
        {!compact && <div className="manual-review-row">
          <span className="manual-review-label"><UserRound />{t("manualReviewUser")}</span>
          <strong>{review.user_display_name || review.user_email}</strong>
          <span className="muted">{review.user_email}</span>
        </div>}
        <div className="manual-review-command">
          <span className="manual-review-label">{t("manualReviewCommand")}</span>
          <HighlightedCommand command={review.command} />
        </div>
        <div className="manual-review-reason">
          <span className="manual-review-label">{t("manualReviewReason")}</span>
          <p>{review.reason}</p>
        </div>
        {item.error && <div className="manual-review-result denied">{item.error}</div>}
      </div>
      {!isDone && <div className="manual-review-auto-allow">
        <label>
          <input
            type="checkbox"
            checked={autoAllowEnabled}
            disabled={isSubmitting}
            onChange={(event) => setAutoAllowEnabled(event.target.checked)}
          />
          <span>{t("manualReviewAutoAllow")}</span>
        </label>
        <input
          type="number"
          min="1"
          max="1440"
          step="1"
          value={autoAllowMinutes}
          disabled={!autoAllowEnabled || isSubmitting}
          aria-label={t("manualReviewMinutes")}
          onChange={(event) => setAutoAllowMinutes(Number(event.target.value))}
        />
        <span>{t("manualReviewMinutes")}</span>
      </div>}
      <div className="manual-review-actions">
        {!isDone ? (
          <>
            <button
              type="button"
              className="primary"
              onClick={() => onAllow(rememberedMinutes)}
              disabled={status !== "pending" || isSubmitting || (autoAllowEnabled && !validAutoAllowMinutes)}
            >
              <Check />{item.submitting === "allow" ? t("manualReviewSubmitting") : t("manualReviewAllow")}
            </button>
            <button type="button" className="danger" onClick={() => onDeny(rememberedMinutes)} disabled={status !== "pending" || isSubmitting || (autoAllowEnabled && !validAutoAllowMinutes)}>
              <X />{item.submitting === "deny" ? t("manualReviewSubmitting") : t("manualReviewDeny")}
            </button>
          </>
        ) : (
          <span className={`manual-review-result ${status}`}>
            {status === "allowed" ? <><Check />{t("manualReviewAllowed")}</> : <><X />{t("manualReviewDenied")}</>}
          </span>
        )}
      </div>
    </div>
  );
}

function HighlightedCommand({ command }: { command: string }) {
  return (
    <code className="manual-review-command-code" aria-label={command}>
      {highlightCommand(command).map((token, index) =>
        token.kind === "space" ? token.text : (
          <span key={`${index}-${token.text}`} className={`cmd-token cmd-${token.kind}`}>
            {token.text}
          </span>
        )
      )}
    </code>
  );
}

type CommandTokenKind = "space" | "command" | "danger" | "flag" | "string" | "variable" | "path" | "operator" | "assignment" | "text";

const shellOperators = new Set(["|", "||", "&", "&&", ";", "(", ")", "<", ">", ">>", "2>", "2>>", "2>&1"]);
const dangerousCommands = new Set([
  "chmod",
  "chown",
  "dd",
  "mkfs",
  "mount",
  "mv",
  "nc",
  "ncat",
  "reboot",
  "rm",
  "shutdown",
  "sudo",
  "su",
  "tee",
  "umount",
]);

function highlightCommand(command: string): Array<{ text: string; kind: CommandTokenKind }> {
  const parts = command.match(/\s+|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`(?:\\.|[^`\\])*`|\|\||&&|2>&1|2>>|>>|2>|[|;&()<>]|[^\s|;&()<>]+/g) ?? [command];
  let expectCommand = true;
  return parts.map((text) => {
    const kind = classifyCommandToken(text, expectCommand);
    if (kind === "operator") {
      expectCommand = text !== ")" && text !== ">" && text !== ">>" && text !== "<" && text !== "2>" && text !== "2>>" && text !== "2>&1";
    } else if (kind !== "space" && kind !== "assignment") {
      expectCommand = false;
    }
    return { text, kind };
  });
}

function classifyCommandToken(text: string, expectCommand: boolean): CommandTokenKind {
  const bare = text.replace(/^['"`]|['"`]$/g, "");
  const normalized = bare.split(/[\\/]/).pop()?.toLowerCase() ?? bare.toLowerCase();
  if (/^\s+$/.test(text)) return "space";
  if (shellOperators.has(text)) return "operator";
  if (/^[A-Za-z_][A-Za-z0-9_]*=/.test(text)) return "assignment";
  if (/^(['"`]).*\1$/.test(text)) return "string";
  if (dangerousCommands.has(normalized)) return "danger";
  if (expectCommand) return "command";
  if (/^--?[\w-]+(?:=.*)?$/.test(text)) return "flag";
  if (/^\$[{A-Za-z_]/.test(text) || /\$\{?[A-Za-z_][A-Za-z0-9_]*}?/.test(text)) return "variable";
  if (/^(?:\.{1,2}\/|\/|~\/|[A-Za-z]:[\\/])/.test(text)) return "path";
  return "text";
}

function canReviewInOrg(data: ConsoleData): boolean {
  if (data.user.is_system_admin) return true;
  const role = data.activeOrg.role;
  return role === "owner" || role === "admin";
}

function notifyPendingReviews(reviews: ManualReview[], notifiedIDs: Set<string>, notifications: Map<string, ReviewNotification>, t: (key: string, fallback?: string) => string) {
  if (!reviews.length || typeof window === "undefined" || typeof document === "undefined") return;
  if (document.visibilityState === "visible" && document.hasFocus()) return;
  if (!("Notification" in window)) return;
  if (Notification.permission !== "granted") return;

  const pending = reviews.filter((review) => !notifiedIDs.has(review.id));
  if (!pending.length) return;

  for (const review of pending) {
    notifiedIDs.add(review.id);
    const notification = new Notification(t("manualReviewTitle"), {
      body: `${review.target_name || review.target_alias}\n${review.command}`,
      tag: `gosshd-manual-review-${review.id}`,
      requireInteraction: true,
    });
    const closeAfterMs = new Date(review.expires_at).getTime() - Date.now();
    if (closeAfterMs <= 0) {
      notification.close();
      continue;
    }
    notifications.set(review.id, { notification, expiresAt: Date.now() + closeAfterMs });
    window.setTimeout(() => closeReviewNotification(notifications, review.id), closeAfterMs);
    notification.onclick = () => {
      window.focus();
      closeReviewNotification(notifications, review.id);
    };
    notification.onclose = () => notifications.delete(review.id);
  }
}

function closeExpiredReviewNotifications(notifications: Map<string, ReviewNotification>) {
  const now = Date.now();
  for (const [id, item] of notifications) {
    if (item.expiresAt <= now) {
      closeReviewNotification(notifications, id);
    }
  }
}

function closeReviewNotification(notifications: Map<string, ReviewNotification>, id: string) {
  const item = notifications.get(id);
  if (!item) return;
  item.notification.close();
  notifications.delete(id);
}

function closeReviewNotifications(notifications: Map<string, ReviewNotification>) {
  for (const item of notifications.values()) {
    item.notification.close();
  }
  notifications.clear();
}

function getNotificationPermission(): NotificationPermission | "unsupported" {
  if (typeof window === "undefined" || !("Notification" in window) || !window.isSecureContext) return "unsupported";
  return Notification.permission;
}

async function requestNotificationPermission(): Promise<NotificationPermission | "unsupported"> {
  if (typeof window === "undefined" || !("Notification" in window) || !window.isSecureContext) return "unsupported";
  return Notification.requestPermission();
}

function secondsUntil(iso: string): number {
  const delta = new Date(iso).getTime() - Date.now();
  return Math.max(0, Math.ceil(delta / 1000));
}

function isReviewActive(review: ManualReview): boolean {
  return secondsUntil(review.expires_at) > 0;
}

function mergeReviews(prev: ReviewState[], incoming: ManualReview[]): ReviewState[] {
  const map = new Map(prev.filter((item) => item.status !== "pending" || isReviewActive(item.review)).map((item) => [item.review.id, item]));
  for (const review of incoming) {
    if (!isReviewActive(review)) continue;
    const current = map.get(review.id);
    map.set(review.id, current ? { ...current, review } : { review, status: "pending" });
  }
  return Array.from(map.values());
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

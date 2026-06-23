import { useMutation } from "@tanstack/react-query";
import { Check, Clock, Server, ShieldAlert, UserRound, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { ConsoleData, ManualReview } from "../types";

type ReviewState = {
  review: ManualReview;
  status: "pending" | "allowed" | "denied" | "expired";
};

const POLL_BACKOFF_MS = 500;
const POLL_MAX_BACKOFF_MS = 5000;

export function ManualReviewPoller({ data }: { data: ConsoleData }) {
  const { t } = useI18n();
  const [reviews, setReviews] = useState<ReviewState[]>([]);
  const [dismissing, setDismissing] = useState<Set<string>>(new Set());
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const knownIDsRef = useRef<Set<string>>(new Set());
  const canReview = useMemo(() => canReviewInOrg(data), [data]);

  useEffect(() => {
    setReviews([]);
    setDismissing(new Set());
    setHidden(new Set());
    knownIDsRef.current = new Set();
  }, [canReview, data.activeOrg.id]);

  useEffect(() => {
    if (!canReview) return;
    let backoff = POLL_BACKOFF_MS;
    let cancelled = false;

    const run = async () => {
      while (!cancelled) {
        try {
          const result = await api.manualReviews(data.activeOrg.id, 25, Array.from(knownIDsRef.current));
          if (cancelled) return;
          for (const review of result.reviews) {
            knownIDsRef.current.add(review.id);
          }
          setReviews((prev) => mergeReviews(prev, result.reviews));
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
  }, [canReview, data.activeOrg.id]);

  const decide = useMutation({
    mutationFn: ({ id, allow }: { id: string; allow: boolean }) => api.decideManualReview(id, allow),
    onSuccess: (_data, variables) => {
      setReviews((prev) =>
        prev.map((item) =>
          item.review.id === variables.id ? { ...item, status: variables.allow ? "allowed" : "denied" } : item
        )
      );
      setDismissing((prev) => new Set(prev).add(variables.id));
      window.setTimeout(() => {
        setHidden((prev) => new Set(prev).add(variables.id));
      }, 1200);
    },
  });

  const visible = reviews.filter((item) => !hidden.has(item.review.id));

  if (!visible.length) return null;

  return (
    <div className="manual-review-toasts" aria-live="polite" aria-atomic="false">
      {visible.map((item) => (
        <ReviewCard
          key={item.review.id}
          item={item}
          dismissing={dismissing.has(item.review.id)}
          onAllow={() => decide.mutate({ id: item.review.id, allow: true })}
          onDeny={() => decide.mutate({ id: item.review.id, allow: false })}
          onExpire={() => {
            setReviews((prev) => prev.map((review) => review.review.id === item.review.id ? { ...review, status: "expired" } : review));
            setDismissing((prev) => new Set(prev).add(item.review.id));
            window.setTimeout(() => setHidden((prev) => new Set(prev).add(item.review.id)), 1200);
          }}
          onDismiss={() => setHidden((prev) => new Set(prev).add(item.review.id))}
          t={t}
        />
      ))}
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
  t,
}: {
  item: ReviewState;
  dismissing: boolean;
  onAllow: () => void;
  onDeny: () => void;
  onExpire: () => void;
  onDismiss: () => void;
  t: (key: string, fallback?: string) => string;
}) {
  const { review, status } = item;
  const [secondsLeft, setSecondsLeft] = useState(() => secondsUntil(review.expires_at));
  const intervalRef = useRef<number | null>(null);

  useEffect(() => {
    intervalRef.current = window.setInterval(() => {
      setSecondsLeft((prev) => {
        const next = Math.max(0, prev - 1);
        return next;
      });
    }, 1000);
    return () => {
      if (intervalRef.current) window.clearInterval(intervalRef.current);
    };
  }, []);

  useEffect(() => {
    if (secondsLeft === 0 && status === "pending") {
      onExpire();
    }
  }, [secondsLeft, status, onExpire]);

  const isDone = status === "allowed" || status === "denied";
  const isExpired = status === "expired" || secondsLeft === 0;

  return (
    <div className={`manual-review-card ${isDone ? "done" : ""} ${isExpired ? "expired" : ""} ${dismissing ? "dismissing" : ""}`}>
      <div className="manual-review-header">
        <ShieldAlert />
        <strong>{t("manualReviewTitle")}</strong>
        <span className="manual-review-countdown">
          <Clock />
          {isExpired ? t("manualReviewExpired") : `${secondsLeft}${t("manualReviewSecondsLeft")}`}
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
        <div className="manual-review-row">
          <span className="manual-review-label"><UserRound />{t("manualReviewUser")}</span>
          <strong>{review.user_display_name || review.user_email}</strong>
          <span className="muted">{review.user_email}</span>
        </div>
        <div className="manual-review-command">
          <span className="manual-review-label">{t("manualReviewCommand")}</span>
          <HighlightedCommand command={review.command} />
        </div>
        <div className="manual-review-reason">
          <span className="manual-review-label">{t("manualReviewReason")}</span>
          <p>{review.reason}</p>
        </div>
      </div>
      <div className="manual-review-actions">
        {!isDone ? (
          <>
            <button type="button" className="primary" onClick={onAllow} disabled={status !== "pending"}>
              <Check />{t("manualReviewAllow")}
            </button>
            <button type="button" className="danger" onClick={onDeny} disabled={status !== "pending"}>
              <X />{t("manualReviewDeny")}
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

function secondsUntil(iso: string): number {
  const delta = new Date(iso).getTime() - Date.now();
  return Math.max(0, Math.ceil(delta / 1000));
}

function mergeReviews(prev: ReviewState[], incoming: ManualReview[]): ReviewState[] {
  const map = new Map(prev.map((item) => [item.review.id, item]));
  for (const review of incoming) {
    const current = map.get(review.id);
    map.set(review.id, current ? { ...current, review } : { review, status: "pending" });
  }
  return Array.from(map.values());
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

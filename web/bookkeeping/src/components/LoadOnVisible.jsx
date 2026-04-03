import { useEffect, useRef, useState } from "react";

export default function LoadOnVisible({ children, placeholder = null, rootMargin = "240px 0px" }) {
  const targetRef = useRef(null);
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    const element = targetRef.current;
    if (!element || visible) return () => {};
    if (typeof window === "undefined" || typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return () => {};
    }

    const observer = new IntersectionObserver(
      (entries) => {
        const hit = entries.some((entry) => entry.isIntersecting);
        if (!hit) return;
        setVisible(true);
        observer.disconnect();
      },
      { rootMargin }
    );
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [rootMargin, visible]);

  return <div ref={targetRef}>{visible ? children : placeholder}</div>;
}

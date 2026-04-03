import { LeftOutlined } from "@ant-design/icons";
import { Button } from "antd";

export default function MobileHeader({ title, onBack, right = null }) {
  return (
    <header className="mobile-header">
      <div className="mobile-header-side">
        {onBack ? (
          <Button
            type="text"
            className="mobile-header-back"
            icon={<LeftOutlined />}
            onClick={onBack}
          />
        ) : (
          <span className="mobile-header-empty" />
        )}
      </div>
      <h1 className="mobile-header-title">{title}</h1>
      <div className="mobile-header-side mobile-header-right">{right || <span className="mobile-header-empty" />}</div>
    </header>
  );
}

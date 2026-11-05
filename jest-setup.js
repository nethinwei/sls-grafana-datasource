// Jest setup provided by Grafana scaffolding
import './.config/jest-setup';

// React's scheduler keeps a MessageChannel port open when @grafana/ui is imported, which stops Jest
// from exiting. Without MessageChannel it falls back to setTimeout.
delete global.MessageChannel;

-- Label colors may carry opacity: #rrggbb or #rrggbbaa.
alter table labels drop constraint labels_color_check;
alter table labels add constraint labels_color_check check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$');

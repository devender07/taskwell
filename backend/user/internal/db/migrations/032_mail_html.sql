-- An email's HTML part; null sends plain text only. body stays the plain-text part.
alter table outbound_mail add column html text;
